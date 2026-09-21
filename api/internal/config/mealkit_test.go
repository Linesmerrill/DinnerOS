package config

import (
	"strings"
	"testing"
)

// validKey is test key material, not a secret.
const validKey = "c29tZS12ZXJ5LWxvbmcta2V5LW1hdGVyaWFsLWZvci10ZXN0cy1vbmx5LXg="

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

func TestMealKitImportRefusesToStartWithoutItsEncryptionKey(t *testing.T) {
	_, err := loadWith(t, map[string]string{"MEAL_KIT_IMPORT_ENABLED": "true"})
	if err == nil {
		t.Fatal("Load() accepted the feature with no encryption key")
	}
	if !strings.Contains(err.Error(), "RECIPE_IMPORT_ENCRYPTION_KEY is required") {
		t.Errorf("error = %v; it should name the missing variable", err)
	}
}

func TestMealKitImportLoadsItsKeyWithoutEverQuotingIt(t *testing.T) {
	cfg, err := loadWith(t, map[string]string{
		"MEAL_KIT_IMPORT_ENABLED":      "true",
		"RECIPE_IMPORT_ENCRYPTION_KEY": validKey,
		"MEAL_KIT_RECIPES_PER_RUN":     "25",
	})
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if !cfg.MealKitImport.Active() || len(cfg.MealKitImport.EncryptionKey) < MinImportKeyBytes {
		t.Fatalf("meal-kit import = %+v", cfg.MealKitImport)
	}
	if cfg.MealKitImport.RecipesPerRun != 25 {
		t.Errorf("recipesPerRun = %d", cfg.MealKitImport.RecipesPerRun)
	}
	// The key never reaches a log line.
	if logged := cfg.LogValue().String(); strings.Contains(logged, validKey) {
		t.Errorf("the logged config carries the encryption key: %s", logged)
	}
}

func TestMealKitImportRejectsBadSettings(t *testing.T) {
	for name, env := range map[string]map[string]string{
		"short key": {"MEAL_KIT_IMPORT_ENABLED": "true", "RECIPE_IMPORT_ENCRYPTION_KEY": "abc"},
		"bad flag":  {"MEAL_KIT_IMPORT_ENABLED": "yes please"},
		"bad cap":   {"MEAL_KIT_RECIPES_PER_RUN": "0"},
		"huge cap":  {"MEAL_KIT_RECIPES_PER_RUN": "5000"},
		"http base": {"MEAL_KIT_HELLOFRESH_BASE_URL": "http://insecure.example.com"},
	} {
		if _, err := loadWith(t, env); err == nil {
			t.Errorf("%s was accepted", name)
		} else if strings.Contains(err.Error(), "abc") && name == "short key" {
			t.Errorf("%s quotes the key: %v", name, err)
		}
	}
}
