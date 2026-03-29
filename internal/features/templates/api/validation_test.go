package templates

import (
	"testing"

	"go.mongodb.org/mongo-driver/bson"
)

func TestVerifyTemplateCandidateFlagsInvalidPayload(t *testing.T) {
	result := verifyTemplateCandidate(bson.M{
		"id":          "tpl-invalid",
		"description": "missing required fields",
	})

	verification, _ := result["verification"].(bson.M)
	if verification["status"] != "invalid" {
		t.Fatalf("verification status = %v, want invalid", verification["status"])
	}
	issues, _ := verification["issues"].([]bson.M)
	if len(issues) == 0 {
		t.Fatalf("expected invalid payload issues, got none")
	}
}

func TestVerifyTemplateCandidateProducesWarningsForWeakDefaults(t *testing.T) {
	result := verifyTemplateCandidate(bson.M{
		"id":          "tpl-weak",
		"label":       "Weak static site",
		"type":        "static-site",
		"repoMode":    "existing",
		"description": "static site starter",
	})

	verification, _ := result["verification"].(bson.M)
	if verification["status"] != "needs-review" {
		t.Fatalf("verification status = %v, want needs-review", verification["status"])
	}
	issues, _ := verification["issues"].([]bson.M)
	if len(issues) == 0 {
		t.Fatalf("expected verification issues, got none")
	}
}

func TestVerifyTemplateCandidateMarksStrongTemplateAsVerified(t *testing.T) {
	result := verifyTemplateCandidate(bson.M{
		"id":          "tpl-verified",
		"label":       "Verified API",
		"type":        "microservice",
		"repoMode":    "template",
		"description": "starter api",
		"category":    "Services",
		"owner":       "releasea",
		"bestFor":     "Internal APIs",
		"setupTime":   "5 min",
		"tier":        "core",
		"templateSource": bson.M{
			"owner": "releasea",
			"repo":  "templates",
			"path":  "api",
		},
		"templateDefaults": bson.M{
			"port":            "8080",
			"healthCheckPath": "/healthz",
		},
	})

	verification, _ := result["verification"].(bson.M)
	if verification["status"] != "verified" {
		t.Fatalf("verification status = %v, want verified", verification["status"])
	}
}

func TestVerifyTemplateCandidateAcceptsCookbookServiceTemplate(t *testing.T) {
	result := verifyTemplateCandidate(bson.M{
		"id":           "go-api",
		"label":        "Go API",
		"type":         "microservice",
		"templateKind": "service",
		"repoMode":     "template",
		"description":  "starter api",
		"category":     "Compute",
		"owner":        "Platform team",
		"bestFor":      "Internal APIs and backend services",
		"setupTime":    "About 5 min",
		"tier":         "Core",
		"templateSource": bson.M{
			"owner": "releasea",
			"repo":  "templates",
			"path":  "go-api",
		},
		"templateDefaults": bson.M{
			"serviceName":     "payments-api",
			"port":            "8080",
			"healthCheckPath": "/healthz",
			"dockerCommand":   "./server",
		},
	})

	verification, _ := result["verification"].(bson.M)
	if verification["status"] != "verified" {
		t.Fatalf("cookbook service verification status = %v, want verified", verification["status"])
	}
}

func TestVerifyTemplateCandidateAcceptsCookbookStaticSiteTemplate(t *testing.T) {
	result := verifyTemplateCandidate(bson.M{
		"id":           "static-site",
		"label":        "Static site",
		"type":         "static-site",
		"templateKind": "service",
		"repoMode":     "template",
		"description":  "frontend starter",
		"category":     "Frontend",
		"owner":        "Web platform",
		"bestFor":      "SPAs, dashboards, documentation sites",
		"setupTime":    "About 3 min",
		"tier":         "Core",
		"templateSource": bson.M{
			"owner": "releasea",
			"repo":  "templates",
			"path":  "static-site",
		},
		"templateDefaults": bson.M{
			"serviceName":    "marketing-site",
			"framework":      "vite",
			"installCommand": "npm install",
			"buildCommand":   "npm run build",
			"outputDir":      "dist",
			"cacheTtl":       "3600",
		},
	})

	verification, _ := result["verification"].(bson.M)
	if verification["status"] != "verified" {
		t.Fatalf("cookbook static site verification status = %v, want verified", verification["status"])
	}
}
