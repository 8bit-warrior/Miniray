package locale

import (
	"encoding/json"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strconv"
	"sync"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/nicksnyder/go-i18n/v2/i18n"
	"golang.org/x/text/language"
)

func translationBundle(t testing.TB, unmarshal i18n.UnmarshalFunc) *i18n.Bundle {
	t.Helper()
	bundle := i18n.NewBundle(language.MustParse("en-US"))
	bundle.RegisterUnmarshalFunc("json", unmarshal)
	if err := parseTranslationFiles(os.DirFS(".."), bundle); err != nil {
		t.Fatal(err)
	}
	return bundle
}

func TestBackendTranslationsMatchFullCatalog(t *testing.T) {
	full := translationBundle(t, json.Unmarshal)
	compact := translationBundle(t, unmarshalBackendMessages)
	for _, tag := range full.LanguageTags() {
		original := i18n.NewLocalizer(full, tag.String())
		optimized := i18n.NewLocalizer(compact, tag.String())
		for _, id := range backendMessageIDs {
			cfg := &i18n.LocalizeConfig{MessageID: id}
			want, originalErr := original.Localize(cfg)
			got, err := optimized.Localize(cfg)
			if (err != nil) != (originalErr != nil) || got != want {
				t.Errorf("%s %s = %q, %v; want %q, %v", tag, id, got, err, want, originalErr)
			}
		}
	}
	// Unsupported languages retain the English fallback.
	original := i18n.NewLocalizer(full, "de-DE")
	optimized := i18n.NewLocalizer(compact, "de-DE")
	cfg := &i18n.LocalizeConfig{MessageID: "pages.login.loginAgain"}
	want, _ := original.Localize(cfg)
	got, _ := optimized.Localize(cfg)
	if got != want {
		t.Fatalf("fallback changed: %q != %q", got, want)
	}
}

func TestBackendCatalogCoversControllerMessages(t *testing.T) {
	err := filepath.WalkDir("../controller", func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() || filepath.Ext(path) != ".go" {
			return nil
		}
		file, err := parser.ParseFile(token.NewFileSet(), path, nil, 0)
		if err != nil {
			return err
		}
		ast.Inspect(file, func(node ast.Node) bool {
			call, ok := node.(*ast.CallExpr)
			if !ok {
				return true
			}
			fn, ok := call.Fun.(*ast.Ident)
			if !ok || fn.Name != "I18nWeb" {
				return true
			}
			literal, ok := call.Args[1].(*ast.BasicLit)
			if !ok {
				t.Errorf("%s: dynamic backend message needs an explicit catalog entry", path)
				return true
			}
			id, err := strconv.Unquote(literal.Value)
			if err != nil {
				t.Error(err)
				return true
			}
			if !slices.Contains(backendMessageIDs, id) {
				t.Errorf("%s: backend message %q missing from catalog", path, id)
			}
			return true
		})
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestConcurrentRequestsKeepTheirLanguage(t *testing.T) {
	if err := InitLocalizer(os.DirFS("..")); err != nil {
		t.Fatal(err)
	}
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	engine.Use(LocalizerMiddleware())
	arrived := make(chan struct{}, 2)
	release := make(chan struct{})
	engine.GET("/", func(c *gin.Context) {
		arrived <- struct{}{}
		<-release
		fn := c.MustGet("I18n").(func(I18nType, string, ...string) string)
		c.String(http.StatusOK, fn(Web, "pages.login.loginAgain"))
	})
	var wg sync.WaitGroup
	for _, lang := range []string{"en-US", "zh-CN"} {
		wg.Go(func() {
			request := httptest.NewRequest(http.MethodGet, "/", nil)
			request.Header.Set("Accept-Language", lang)
			response := httptest.NewRecorder()
			engine.ServeHTTP(response, request)
			want := localize(LocalizerFor(lang), "pages.login.loginAgain")
			if response.Body.String() != want {
				t.Errorf("%s response %q, want %q", lang, response.Body.String(), want)
			}
		})
	}
	<-arrived
	<-arrived
	close(release)
	wg.Wait()
}

func TestBackendCatalogMemory(t *testing.T) {
	retained := func(unmarshal i18n.UnmarshalFunc) uint64 {
		runtime.GC()
		var before runtime.MemStats
		runtime.ReadMemStats(&before)
		bundle := translationBundle(t, unmarshal)
		runtime.GC()
		var after runtime.MemStats
		runtime.ReadMemStats(&after)
		runtime.KeepAlive(bundle)
		if after.HeapAlloc < before.HeapAlloc {
			return 0
		}
		return after.HeapAlloc - before.HeapAlloc
	}
	full := retained(json.Unmarshal)
	compact := retained(unmarshalBackendMessages)
	t.Logf("13-language retained Go heap: full=%d KiB backend=%d KiB", full/1024, compact/1024)
	if compact >= full {
		t.Fatalf("backend catalog did not reduce retained heap")
	}
}
