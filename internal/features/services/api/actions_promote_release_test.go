package services

import (
	"testing"

	"go.mongodb.org/mongo-driver/bson"
)

func TestSelectPromotedReleaseReusesSourceImage(t *testing.T) {
	resolution, err := selectPromotedRelease("dev", "staging", bson.M{
		"id":     "deploy-1",
		"commit": "ca9e0451abcd",
		"image":  "registry.local/releasea/go-service:ca9e0451",
	}, nil)
	if err != nil {
		t.Fatalf("select promoted release: %v", err)
	}
	if !resolution.ReuseImage || resolution.Image != "registry.local/releasea/go-service:ca9e0451" {
		t.Fatalf("expected reusable image, got %+v", resolution)
	}
	if resolution.Version != "ca9e0451abcd" || resolution.SourceEnvironment != "dev" || resolution.SourceDeployID != "deploy-1" {
		t.Fatalf("expected source release identity, got %+v", resolution)
	}
}

func TestSelectPromotedReleaseFallsBackToBuild(t *testing.T) {
	resolution, err := selectPromotedRelease("staging", "prod", bson.M{
		"id":     "deploy-2",
		"commit": "abc123",
	}, []bson.M{
		{"commit": "other", "image": "registry.local/releasea/go-service:other"},
		{"commit": "abc123", "image": "registry.local/releasea/go-service:abc123"},
	})
	if err != nil {
		t.Fatalf("select promoted release: %v", err)
	}
	if resolution.Image != "registry.local/releasea/go-service:abc123" || resolution.Version != "abc123" {
		t.Fatalf("expected matching build image, got %+v", resolution)
	}
}

func TestSelectPromotedReleaseRejectsSameEnvironment(t *testing.T) {
	_, err := selectPromotedRelease("dev", "dev", bson.M{"image": "registry.local/app:1"}, nil)
	if err == nil {
		t.Fatal("expected same-environment promotion to fail")
	}
}

func TestSelectFirstPromotedReleaseSkipsDeployWithoutImage(t *testing.T) {
	resolution, err := selectFirstPromotedRelease("dev", "staging", []bson.M{
		{"id": "deploy-new", "commit": "3737949c"},
		{"id": "deploy-old", "commit": "ca9e0451", "image": "registry.local/releasea/go-service:ca9e0451"},
	}, func(deploy bson.M) ([]bson.M, error) {
		return nil, nil
	})
	if err != nil {
		t.Fatalf("select first promoted release: %v", err)
	}
	if resolution.SourceDeployID != "deploy-old" || resolution.Version != "ca9e0451" {
		t.Fatalf("expected the older reusable release, got %+v", resolution)
	}
}

func TestSelectPromotedReleaseRequiresImage(t *testing.T) {
	_, err := selectPromotedRelease("dev", "staging", bson.M{"commit": "abc123"}, nil)
	if err == nil {
		t.Fatal("expected missing image to fail")
	}
}
