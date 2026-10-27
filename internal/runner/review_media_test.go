package runner

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"image"
	"image/png"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"coordica/internal/domain"
	"coordica/internal/gitops"
)

func largeReviewMediaFixture(t *testing.T) (*Runner, string, domain.Assignment) {
	t.Helper()
	ctx := context.Background()
	g := gitops.New("git")
	repo := filepath.Join(t.TempDir(), "fixed source")
	if err := g.Init(ctx, repo, false); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo, "source.ts"), []byte("export const value = 1;\n"), 0600); err != nil {
		t.Fatal(err)
	}
	base, err := g.CommitAll(ctx, repo, "baseline", gitops.Identity{})
	if err != nil {
		t.Fatal(err)
	}
	pixels := image.NewNRGBA(image.Rect(0, 0, 640, 640))
	if _, err = rand.Read(pixels.Pix); err != nil {
		t.Fatal(err)
	}
	var picture bytes.Buffer
	if err = png.Encode(&picture, pixels); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(repo, "screenshot 大图.png"), picture.Bytes(), 0600); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(repo, "source.ts"), []byte("export const value = 2;\n"), 0600); err != nil {
		t.Fatal(err)
	}
	head, err := g.CommitAll(ctx, repo, "code and binary evidence", gitops.Identity{})
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(http.NotFoundHandler())
	t.Cleanup(server.Close)
	a := domain.Assignment{Run: domain.Run{Kind: "review", Base: head.String()}, Candidate: &domain.Candidate{Main: base.String(), Commit: head.String()}}
	return testRunner(t, server), repo, a
}

func TestLargeFixedMediaReviewRetainsCompleteBoundedMaterial(t *testing.T) {
	r, repo, a := largeReviewMediaFixture(t)
	input, err := r.reviewInput(context.Background(), a, repo)
	if err != nil {
		t.Fatal("fixed media must be available without inflating the text prompt", err)
	}
	if len(input) > maxReviewInput || !strings.Contains(input, "+export const value = 2") || !strings.Contains(input, "screenshot 大图.png") {
		t.Fatal("text or changed media disappeared from the review")
	}
	files, err := filepath.Glob(filepath.Join(filepath.Dir(repo), "review-material-*", "complete.patch"))
	if err != nil || len(files) != 1 {
		t.Fatal("complete material was not retained outside the fixed worktree", files, err)
	}
	full, err := r.git.DiffPatch(context.Background(), repo, gitops.OID(a.Candidate.Main), gitops.OID(a.Run.Base))
	if err != nil || len(full) <= maxReviewInput {
		t.Fatal("fixture does not exceed inline cap", err)
	}
	stored, err := os.ReadFile(files[0])
	if err != nil || !bytes.Equal(stored, full) {
		t.Fatal("fixed complete patch was truncated or rewritten", err)
	}
	digest := sha256.Sum256(stored)
	encodedPath, _ := json.Marshal(files[0])
	if !strings.Contains(input, hex.EncodeToString(digest[:])) || !strings.Contains(input, string(encodedPath)) {
		t.Fatal("material path or fixed digest missing")
	}
	if err = r.git.VerifyClean(context.Background(), repo, gitops.OID(a.Run.Base)); err != nil {
		t.Fatal("review material modified fixed candidate", err)
	}
	pictures, _ := filepath.Glob(filepath.Join(filepath.Dir(files[0]), "image-*"))
	if len(pictures) != 1 {
		t.Fatal("fixed image bytes unavailable to a read-only reviewer", pictures)
	}
	imageBytes, err := os.ReadFile(pictures[0])
	original, readErr := os.ReadFile(filepath.Join(repo, "screenshot 大图.png"))
	if err != nil || readErr != nil || !bytes.Equal(imageBytes, original) {
		t.Fatal("material image is not the fixed Git blob", err, readErr)
	}
}

func TestLargeMediaReviewStillRejectsHiddenOrOversizedCode(t *testing.T) {
	for _, scenario := range []string{"large text", "unknown binary", "binary-marked source", "fake image"} {
		t.Run(scenario, func(t *testing.T) {
			r, repo, a := largeReviewMediaFixture(t)
			name, data := "large.ts", []byte(strings.Repeat("export const longCode = 1;\n", maxReviewInput/20))
			switch scenario {
			case "unknown binary":
				name, data = "program.wasm", []byte("\x00unknown executable")
			case "binary-marked source":
				name, data = "hidden.js", []byte("hidden code\n")
				if err := os.WriteFile(filepath.Join(repo, ".gitattributes"), []byte("*.js -diff\n"), 0600); err != nil {
					t.Fatal(err)
				}
			case "fake image":
				name, data = "hidden.png", []byte("\x00this is not an image")
			}
			if err := os.WriteFile(filepath.Join(repo, name), data, 0600); err != nil {
				t.Fatal(err)
			}
			head, err := r.git.CommitAll(context.Background(), repo, scenario, gitops.Identity{})
			if err != nil {
				t.Fatal(err)
			}
			a.Run.Base, a.Candidate.Commit = head.String(), head.String()
			if input, err := r.reviewInput(context.Background(), a, repo); err == nil || input != "" {
				t.Fatal("unreviewable code/binary became a successful image preview", err)
			}
			left, _ := filepath.Glob(filepath.Join(filepath.Dir(repo), "review-material-*"))
			if len(left) != 0 {
				t.Fatal("failed preparation leaked material", left)
			}
		})
	}
}

func TestLargeDeletedImageReviewRetainsBeforeObject(t *testing.T) {
	r, repo, a := largeReviewMediaFixture(t)
	a.Candidate.Main = a.Candidate.Commit
	if err := os.Remove(filepath.Join(repo, "screenshot 大图.png")); err != nil {
		t.Fatal(err)
	}
	head, err := r.git.CommitAll(context.Background(), repo, "delete image", gitops.Identity{})
	if err != nil {
		t.Fatal(err)
	}
	a.Run.Base, a.Candidate.Commit = head.String(), head.String()
	input, err := r.reviewInput(context.Background(), a, repo)
	if err != nil || !strings.Contains(input, `"side":"before"`) || strings.Contains(input, `"side":"after"`) {
		t.Fatal("deleted image was dropped", err)
	}
}
