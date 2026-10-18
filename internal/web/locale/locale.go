// Package locale provides internationalization (i18n) support for the miniray web panel,
// including translation loading, localization, and request middleware.
package locale

import (
	"io/fs"
	"os"
	"strings"
	"sync"

	"github.com/8bit-warrior/Miniray/internal/logger"

	"github.com/gin-gonic/gin"
	"github.com/nicksnyder/go-i18n/v2/i18n"
	"golang.org/x/text/language"
)

var (
	bundleMu   sync.RWMutex
	i18nBundle *i18n.Bundle
)

// I18nType represents the type of interface for internationalization.
type I18nType string

const (
	Web I18nType = "web" // Web interface type
)

// InitLocalizer initializes the internationalization system with embedded translation files.
func InitLocalizer(i18nFS fs.FS) error {
	bundle := i18n.NewBundle(language.MustParse("en-US"))
	bundle.RegisterUnmarshalFunc("json", unmarshalBackendMessages)
	if err := parseTranslationFiles(i18nFS, bundle); err != nil {
		return err
	}
	bundleMu.Lock()
	i18nBundle = bundle
	bundleMu.Unlock()

	return nil
}

// createTemplateData creates a template data map from parameters with optional separator.
func createTemplateData(params []string, separator ...string) map[string]any {
	sep := "=="
	if len(separator) > 0 {
		sep = separator[0]
	}

	templateData := make(map[string]any)
	for _, param := range params {
		parts := strings.SplitN(param, sep, 2)
		templateData[parts[0]] = parts[1]
	}

	return templateData
}

func localize(localizer *i18n.Localizer, key string, params ...string) string {
	templateData := createTemplateData(params)

	if localizer == nil {
		// Fallback to key if localizer not ready; prevents nil panic on pages like sub
		return key
	}

	msg, err := localizer.Localize(&i18n.LocalizeConfig{
		MessageID:    key,
		TemplateData: templateData,
	})
	if err != nil {
		logger.Errorf("Failed to localize message: %v", err)
		return ""
	}

	return msg
}

// Browser languages are mapped to the only two translations shipped with the panel.
func browserLanguage(raw string) string {
	first := strings.ToLower(strings.TrimSpace(strings.SplitN(raw, ",", 2)[0]))
	first = strings.TrimSpace(strings.SplitN(first, ";", 2)[0])
	if first == "zh" || strings.HasPrefix(first, "zh-") || strings.HasPrefix(first, "zh_") {
		return "zh-CN"
	}
	return "en-US"
}

// LocalizerFor returns a new Localizer for the given language tag using the global bundle.
func LocalizerFor(lang string) *i18n.Localizer {
	bundleMu.RLock()
	defer bundleMu.RUnlock()
	if i18nBundle == nil {
		return nil
	}
	return i18n.NewLocalizer(i18nBundle, browserLanguage(lang))
}

// LocalizerMiddleware returns a Gin middleware that sets up localization for web requests.
// It determines the user's language from the browser Accept-Language header,
// creates a localizer instance, and stores it in the Gin context for use in handlers.
// Also provides the I18n function in the context for template rendering.
func LocalizerMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		bundleMu.Lock()
		if i18nBundle == nil {
			bundle := i18n.NewBundle(language.MustParse("en-US"))
			bundle.RegisterUnmarshalFunc("json", unmarshalBackendMessages)
			if err := loadTranslationsFromDisk(bundle); err != nil {
				logger.Warning("i18n lazy load failed:", err)
			}
			i18nBundle = bundle
		}
		bundleMu.Unlock()
		localizer := LocalizerFor(c.GetHeader("Accept-Language"))
		c.Set("localizer", localizer)
		c.Set("I18n", func(_ I18nType, key string, params ...string) string {
			return localize(localizer, key, params...)
		})
		c.Next()
	}
}

// loadTranslationsFromDisk attempts to load translation files from "internal/web/translation" using the local filesystem.
func loadTranslationsFromDisk(bundle *i18n.Bundle) error {
	return parseTranslationFiles(os.DirFS("internal/web"), bundle)
}

// parseTranslationFiles parses embedded translation files and adds them to the i18n bundle.
func parseTranslationFiles(i18nFS fs.FS, i18nBundle *i18n.Bundle) error {
	err := fs.WalkDir(i18nFS, "translation",
		func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}

			if d.IsDir() {
				return nil
			}

			data, err := fs.ReadFile(i18nFS, path)
			if err != nil {
				return err
			}

			_, err = i18nBundle.ParseMessageFileBytes(data, path)
			return err
		})
	if err != nil {
		return err
	}

	return nil
}
