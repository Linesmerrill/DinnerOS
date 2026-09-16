package recommendations

import (
	"testing"

	"github.com/Linesmerrill/DinnerOS/api/internal/recipes"
)

func TestLongCookCut(t *testing.T) {
	for _, tt := range []struct {
		name   string
		recipe recipes.Recipe
		want   string
	}{
		{"pork shoulder", recipeWith("Smoked Pork Shoulder", "Bone-In Pork Shoulder", "Rub"), "Bone-In Pork Shoulder"},
		{"whole chicken", recipeWith("Beer Can Chicken", "Whole Chicken"), "Whole Chicken"},
		{"spatchcock", recipeWith("Smoked Spatchcock Chicken", "Spatchcock Chicken"), "Spatchcock Chicken"},
		{"ribs", recipeWith("Ribs", "Baby Back Ribs"), "Baby Back Ribs"},
		{"brisket", recipeWith("Brisket", "Beef Brisket"), "Beef Brisket"},
		{"tenderloin is not a long cut", recipeWith("Smoked Pork Tenderloin", "Pork Tenderloin"), ""},
		{"chops are not a long cut", recipeWith("Pork Chops", "Pork Chops"), ""},
		{"pulled shoulder is already cooked", recipeWith("Glazed Pork", "Pulled Pork Shoulder"), ""},
		{"not a smoker dish", recipeWith("Short Rib Ramen", "Short Ribs", "Ramen Noodles"), ""},
	} {
		t.Run(tt.name, func(t *testing.T) {
			a := attributes(tt.recipe, nil, defaultBands)
			if a.LongCookCut != tt.want {
				t.Errorf("long cook cut = %q, want %q", a.LongCookCut, tt.want)
			}
			if item := a.item(tt.recipe); item.LongCook != (tt.want != "") {
				t.Errorf("item long cook = %v", item.LongCook)
			}
		})
	}
}
