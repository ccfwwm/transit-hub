package monitor_media

import (
	"context"
	"os"
	"strings"
	"testing"
)

func TestResolveMediaPresetsUseNativeMinimums(t *testing.T) {
	cap, req, err := Resolve(Options{Model: "gpt-image-2", Mode: "image"})
	if err != nil || cap.MediaType != "image" || req.Parameters["n"] != 1 {
		t.Fatalf("image resolve: cap=%+v req=%+v err=%v", cap, req, err)
	}
	cap, req, err = Resolve(Options{Model: "veo-3.1-fast", Mode: "video"})
	if err != nil || cap.MediaType != "video" || req.Duration == "" {
		t.Fatalf("video resolve: cap=%+v req=%+v err=%v", cap, req, err)
	}
	if _, _, err = Resolve(Options{Model: "unmapped", Mode: "image"}); err == nil {
		t.Fatal("unknown model accepted without adapter")
	}
}

func TestCaptureTestArtifactDecodesAndRejectsInvalidImage(t *testing.T) {
	valid := "data:image/png;base64,iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mNk+A8AAQUBAScY42YAAAAASUVORK5CYII="
	cap := Capability{MediaType: "image"}
	root, err := os.MkdirTemp("", "monitor-media-test-")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(root)
	store := &BlobStore{Root: root}
	artifact, path, hash, err := store.CaptureTestArtifact(context.Background(), nil, NewTaskID(), cap, Artifact{Key: "image1", Type: "image", Source: valid}, Account{})
	if err != nil || !artifact.Available || path == "" || len(hash) != 64 {
		t.Fatalf("valid artifact: %+v path=%s hash=%s err=%v", artifact, path, hash, err)
	}
	_, _, _, err = store.CaptureTestArtifact(context.Background(), nil, NewTaskID(), cap, Artifact{Key: "image1", Type: "image", Source: "data:image/png;base64," + strings.Repeat("A", 32)}, Account{})
	if err == nil {
		t.Fatal("invalid image accepted")
	}
}
