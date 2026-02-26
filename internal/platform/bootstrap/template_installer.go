package bootstrap

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"path"
	"sort"
	"strings"
	"time"

	"releaseaapi/internal/platform/config"
	"releaseaapi/internal/platform/shared"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
	"gopkg.in/yaml.v3"
)

const (
	templateManifestFileName = "releasea.yaml"
	defaultTemplateVersion   = "1.0.0"
	maxManifestSizeBytes     = 1 << 20
)

type templateRepoSource struct {
	Owner string `yaml:"owner"`
	Repo  string `yaml:"repo"`
	Path  string `yaml:"path"`
}

type templateManifestDoc struct {
	APIVersion        string                 `yaml:"apiVersion"`
	Kind              string                 `yaml:"kind"`
	ID                string                 `yaml:"id"`
	Name              string                 `yaml:"name"`
	Version           string                 `yaml:"version"`
	TemplateType      string                 `yaml:"templateType"`
	Description       string                 `yaml:"description"`
	Icon              string                 `yaml:"icon"`
	Category          string                 `yaml:"category"`
	Owner             string                 `yaml:"owner"`
	BestFor           string                 `yaml:"bestFor"`
	Defaults          string                 `yaml:"defaults"`
	SetupTime         string                 `yaml:"setupTime"`
	Tier              string                 `yaml:"tier"`
	Highlights        []string               `yaml:"highlights"`
	RepoMode          string                 `yaml:"repoMode"`
	TemplateKind      string                 `yaml:"templateKind"`
	AllowTemplateMode bool                   `yaml:"allowTemplateToggle"`
	TemplateDefaults  map[string]interface{} `yaml:"templateDefaults"`
	Source            templateRepoSource     `yaml:"source"`
}

type templateManifestEntry struct {
	TemplatePath string
	Manifest     templateManifestDoc
}

func InstallTemplates(cfg *config.Config) error {
	if cfg == nil {
		return errors.New("configuration missing")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	entries, err := fetchTemplateManifests(ctx, cfg)
	if err != nil {
		return err
	}
	if len(entries) == 0 {
		return errors.New("no templates found in repository")
	}

	col := shared.Collection(shared.ServiceTemplatesCollection)
	now := shared.NowISO()
	importedIDs := make([]string, 0, len(entries))

	for _, entry := range entries {
		doc, err := buildTemplateDocument(entry, cfg, now)
		if err != nil {
			return err
		}
		templateID := strings.TrimSpace(shared.StringValue(doc["_id"]))
		if templateID == "" {
			return fmt.Errorf("template id missing for %s", entry.TemplatePath)
		}
		importedIDs = append(importedIDs, templateID)

		existing, err := shared.FindOne(ctx, col, bson.M{"_id": templateID})
		if err != nil && !errors.Is(err, mongo.ErrNoDocuments) {
			return fmt.Errorf("failed to load existing template %s: %w", templateID, err)
		}
		createdAt := strings.TrimSpace(shared.StringValue(existing["createdAt"]))
		if createdAt == "" {
			createdAt = now
		}
		doc["createdAt"] = createdAt

		if _, err := col.ReplaceOne(ctx, bson.M{"_id": templateID}, doc, options.Replace().SetUpsert(true)); err != nil {
			return fmt.Errorf("failed to upsert template %s: %w", templateID, err)
		}
	}

	if _, err := col.DeleteMany(ctx, bson.M{
		"id":                   bson.M{"$nin": importedIDs},
		"templateSource.owner": strings.TrimSpace(cfg.TemplatesRepoOwner),
		"templateSource.repo":  strings.TrimSpace(cfg.TemplatesRepoName),
	}); err != nil {
		return fmt.Errorf("failed to remove stale templates: %w", err)
	}

	log.Printf(
		"[templates] installed %d templates from %s/%s@%s",
		len(importedIDs),
		strings.TrimSpace(cfg.TemplatesRepoOwner),
		strings.TrimSpace(cfg.TemplatesRepoName),
		strings.TrimSpace(cfg.TemplatesRepoRef),
	)
	return nil
}

func fetchTemplateManifests(ctx context.Context, cfg *config.Config) ([]templateManifestEntry, error) {
	owner := strings.TrimSpace(cfg.TemplatesRepoOwner)
	repo := strings.TrimSpace(cfg.TemplatesRepoName)
	ref := strings.TrimSpace(cfg.TemplatesRepoRef)
	if owner == "" || repo == "" {
		return nil, errors.New("template repository owner and name are required")
	}
	if ref == "" {
		ref = "main"
	}

	archiveURL := fmt.Sprintf(
		"https://github.com/%s/%s/archive/refs/heads/%s.tar.gz",
		owner,
		repo,
		ref,
	)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, archiveURL, nil)
	if err != nil {
		return nil, err
	}

	httpClient := &http.Client{Timeout: 45 * time.Second}
	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to download templates archive: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return nil, fmt.Errorf("template repository request failed (%d): %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	gzReader, err := gzip.NewReader(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to open templates archive: %w", err)
	}
	defer gzReader.Close()

	tarReader := tar.NewReader(gzReader)
	entries := make([]templateManifestEntry, 0, 16)
	seenByID := map[string]string{}

	for {
		header, err := tarReader.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("failed to read templates archive: %w", err)
		}
		if header.Typeflag != tar.TypeReg {
			continue
		}

		cleanName := path.Clean(strings.TrimPrefix(header.Name, "./"))
		if cleanName == "." || strings.HasPrefix(cleanName, "../") || path.Base(cleanName) != templateManifestFileName {
			continue
		}

		parts := strings.SplitN(cleanName, "/", 3)
		if len(parts) != 3 || parts[2] != templateManifestFileName {
			continue
		}
		templatePath := strings.TrimSpace(parts[1])
		if templatePath == "" {
			continue
		}

		rawManifest, err := io.ReadAll(io.LimitReader(tarReader, maxManifestSizeBytes))
		if err != nil {
			return nil, fmt.Errorf("failed to read manifest for %s: %w", templatePath, err)
		}
		var manifest templateManifestDoc
		if err := yaml.Unmarshal(rawManifest, &manifest); err != nil {
			return nil, fmt.Errorf("invalid manifest for %s: %w", templatePath, err)
		}

		templateID := strings.TrimSpace(manifest.ID)
		if templateID == "" {
			return nil, fmt.Errorf("manifest missing id for %s", templatePath)
		}
		if previousPath, exists := seenByID[templateID]; exists {
			return nil, fmt.Errorf("duplicate template id %s in %s and %s", templateID, previousPath, templatePath)
		}
		seenByID[templateID] = templatePath
		entries = append(entries, templateManifestEntry{
			TemplatePath: templatePath,
			Manifest:     manifest,
		})
	}

	sort.Slice(entries, func(i, j int) bool {
		return strings.TrimSpace(entries[i].Manifest.ID) < strings.TrimSpace(entries[j].Manifest.ID)
	})
	return entries, nil
}

func buildTemplateDocument(entry templateManifestEntry, cfg *config.Config, now string) (bson.M, error) {
	manifest := entry.Manifest
	kind := strings.TrimSpace(manifest.Kind)
	if kind != "ReleaseaTemplate" {
		return nil, fmt.Errorf("template %s has invalid kind %q", entry.TemplatePath, kind)
	}

	templateType := normalizeTemplateType(manifest.TemplateType)
	if templateType == "" {
		return nil, fmt.Errorf("template %s has invalid templateType %q", entry.TemplatePath, strings.TrimSpace(manifest.TemplateType))
	}

	repoMode := strings.ToLower(strings.TrimSpace(manifest.RepoMode))
	if repoMode == "" {
		repoMode = "template"
	}
	if repoMode != "template" && repoMode != "existing" {
		return nil, fmt.Errorf("template %s has invalid repoMode %q", entry.TemplatePath, repoMode)
	}

	sourceOwner := strings.TrimSpace(manifest.Source.Owner)
	sourceRepo := strings.TrimSpace(manifest.Source.Repo)
	sourcePath := strings.Trim(strings.TrimSpace(manifest.Source.Path), "/")
	if sourceOwner == "" {
		sourceOwner = strings.TrimSpace(cfg.TemplatesRepoOwner)
	}
	if sourceRepo == "" {
		sourceRepo = strings.TrimSpace(cfg.TemplatesRepoName)
	}
	if sourcePath == "" {
		sourcePath = entry.TemplatePath
	}
	if sourceOwner != strings.TrimSpace(cfg.TemplatesRepoOwner) || sourceRepo != strings.TrimSpace(cfg.TemplatesRepoName) {
		return nil, fmt.Errorf("template %s must reference source %s/%s", entry.TemplatePath, strings.TrimSpace(cfg.TemplatesRepoOwner), strings.TrimSpace(cfg.TemplatesRepoName))
	}
	if sourcePath != entry.TemplatePath {
		return nil, fmt.Errorf("template %s has mismatched source.path %q", entry.TemplatePath, sourcePath)
	}

	templateID := strings.TrimSpace(manifest.ID)
	label := strings.TrimSpace(manifest.Name)
	if label == "" {
		label = templateID
	}
	description := strings.TrimSpace(manifest.Description)
	if description == "" {
		description = "Production-ready starter template for Releasea."
	}
	version := strings.TrimSpace(manifest.Version)
	if version == "" {
		version = defaultTemplateVersion
	}

	templateKind := strings.TrimSpace(manifest.TemplateKind)
	metaDefaults := resolveTemplateMetaDefaults(templateType, templateKind)
	icon := stringOrDefault(strings.TrimSpace(manifest.Icon), metaDefaults.Icon)
	category := stringOrDefault(strings.TrimSpace(manifest.Category), metaDefaults.Category)
	owner := stringOrDefault(strings.TrimSpace(manifest.Owner), metaDefaults.Owner)
	bestFor := stringOrDefault(strings.TrimSpace(manifest.BestFor), metaDefaults.BestFor)
	defaults := stringOrDefault(strings.TrimSpace(manifest.Defaults), metaDefaults.Defaults)
	setupTime := stringOrDefault(strings.TrimSpace(manifest.SetupTime), metaDefaults.SetupTime)
	tier := stringOrDefault(strings.TrimSpace(manifest.Tier), metaDefaults.Tier)
	highlights := normalizedHighlights(manifest.Highlights, metaDefaults.Highlights)

	doc := bson.M{
		"_id":         templateID,
		"id":          templateID,
		"type":        templateType,
		"label":       label,
		"description": description,
		"version":     version,
		"icon":        icon,
		"category":    category,
		"owner":       owner,
		"bestFor":     bestFor,
		"defaults":    defaults,
		"setupTime":   setupTime,
		"tier":        tier,
		"highlights":  highlights,
		"repoMode":    repoMode,
		"templateSource": bson.M{
			"owner": sourceOwner,
			"repo":  sourceRepo,
			"path":  sourcePath,
		},
		"allowTemplateToggle": manifest.AllowTemplateMode,
		"schemaVersion":       "v1",
		"createdAt":           now,
		"updatedAt":           now,
	}

	if templateKind != "" {
		doc["templateKind"] = templateKind
	}
	if len(manifest.TemplateDefaults) > 0 {
		doc["templateDefaults"] = manifest.TemplateDefaults
	}
	return doc, nil
}

func normalizeTemplateType(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "microservice":
		return "microservice"
	case "static-site":
		return "static-site"
	default:
		return ""
	}
}

type templateMetaDefaults struct {
	Icon       string
	Category   string
	Owner      string
	BestFor    string
	Defaults   string
	SetupTime  string
	Tier       string
	Highlights []string
}

var (
	defaultMicroserviceMeta = templateMetaDefaults{
		Icon:       "Server",
		Category:   "Compute",
		Owner:      "Platform team",
		BestFor:    "APIs and internal services",
		Defaults:   "Runtime, health checks, and deploy strategy",
		SetupTime:  "About 5 min",
		Tier:       "Core",
		Highlights: []string{"Health checks", "Safe defaults", "Observability"},
	}
	defaultStaticSiteMeta = templateMetaDefaults{
		Icon:       "Globe",
		Category:   "External",
		Owner:      "Web platform",
		BestFor:    "Docs, landing pages, and web experiences",
		Defaults:   "Build pipeline and CDN defaults",
		SetupTime:  "About 3 min",
		Tier:       "External",
		Highlights: []string{"Global CDN", "Preview deploys", "Cache control"},
	}
	defaultScheduledJobMeta = templateMetaDefaults{
		Icon:       "Clock",
		Category:   "Automation",
		Owner:      "Platform team",
		BestFor:    "ETL, reports, and maintenance workloads",
		Defaults:   "Cron schedule and retries",
		SetupTime:  "About 3 min",
		Tier:       "Core",
		Highlights: []string{"Cron schedules", "Retries", "Job history"},
	}
)

func resolveTemplateMetaDefaults(templateType, templateKind string) templateMetaDefaults {
	if strings.EqualFold(templateKind, "scheduled-job") {
		return defaultScheduledJobMeta
	}
	if templateType == "static-site" {
		return defaultStaticSiteMeta
	}
	return defaultMicroserviceMeta
}

func normalizedHighlights(values []string, fallback []string) []string {
	cleaned := make([]string, 0, len(values))
	seen := map[string]struct{}{}
	for _, value := range values {
		trimmed := strings.TrimSpace(value)
		if trimmed == "" {
			continue
		}
		key := strings.ToLower(trimmed)
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}
		cleaned = append(cleaned, trimmed)
	}
	if len(cleaned) > 0 {
		return cleaned
	}
	return append([]string{}, fallback...)
}

func stringOrDefault(value, fallback string) string {
	if strings.TrimSpace(value) != "" {
		return value
	}
	return fallback
}
