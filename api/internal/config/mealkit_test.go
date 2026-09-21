package config

import (
	"strings"
	"testing"
)

func loadWith(t *testing.T, extra map[string]string) (Config, error) {
	t.Helper()
	env := map[string]string{}
	for k, v := range extra {
		env[k] = v
	}
	return Load(func(key string) string { return env[key] })
}

func TestMealKitImportIsOffByDefault(t *testing.T) {
	cfg, err := loadWith(t, nil)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.MealKitImport.Enabled || cfg.MealKitImport.Active() {
		t.Errorf("meal-kit import = %+v, want off", cfg.MealKitImport)
	}
}

// Turning the feature on needs nothing else. It used to need an encryption key
// for the session tokens it stored; it stores nothing about a meal-kit account
// any more, so there is no key, and no way to configure it wrongly.
func TestMealKitImportTurnsOnWithNoSecretToConfigure(t *testing.T) {
	cfg, err := loadWith(t, map[string]string{
		"MEAL_KIT_IMPORT_ENABLED":  "true",
		"MEAL_KIT_RECIPES_PER_RUN": "25",
	})
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if !cfg.MealKitImport.Active() {
		t.Fatalf("meal-kit import = %+v, want active", cfg.MealKitImport)
	}
	if cfg.MealKitImport.RecipesPerRun != 25 {
		t.Errorf("recipesPerRun = %d", cfg.MealKitImport.RecipesPerRun)
	}
}

// A key left behind in a deployment's config from the old design is ignored,
// not a startup failure: removing a config var should not stop the dyno.
func TestMealKitImportIgnoresAnOldEncryptionKey(t *testing.T) {
	const leftover = "c29tZS12ZXJ5LWxvbmcta2V5LW1hdGVyaWFsLWZvci10ZXN0cy1vbmx5LXg="
	cfg, err := loadWith(t, map[string]string{
		"MEAL_KIT_IMPORT_ENABLED":      "true",
		"RECIPE_IMPORT_ENCRYPTION_KEY": leftover,
	})
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if !cfg.MealKitImport.Active() {
		t.Errorf("meal-kit import = %+v", cfg.MealKitImport)
	}
	if logged := cfg.LogValue().String(); strings.Contains(logged, leftover) {
		t.Errorf("the logged config carries the leftover key: %s", logged)
	}
}

func TestMealKitImportRejectsBadSettings(t *testing.T) {
	for name, env := range map[string]map[string]string{
		"bad flag":  {"MEAL_KIT_IMPORT_ENABLED": "yes please"},
		"bad cap":   {"MEAL_KIT_RECIPES_PER_RUN": "0"},
		"huge cap":  {"MEAL_KIT_RECIPES_PER_RUN": "5000"},
		"http base": {"MEAL_KIT_HELLOFRESH_BASE_URL": "http://insecure.example.com"},
	} {
		if _, err := loadWith(t, env); err == nil {
			t.Errorf("%s was accepted", name)
		}
	}
}
