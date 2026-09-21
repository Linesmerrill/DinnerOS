package ingredients

import "testing"

func TestSpicy(t *testing.T) {
	hot := []string{
		"Chili Flakes", "Crushed Red Pepper Flakes", "Cayenne Pepper", "Hot Sauce",
		"Sriracha", "Gochujang", "Jalapeño", "Jalapeno, thinly sliced", "Chipotle Peppers in Adobo",
		"Chili Powder", "Szechuan Paste", "Sichuan Chili Oil", "Harissa Powder",
		"Blackening Spice", "Sambal Oelek", "Habanero", "Spicy Pork Sausage",
	}
	for _, name := range hot {
		if !Spicy(name) {
			t.Errorf("Spicy(%q) = false, want true", name)
		}
	}
	mild := []string{
		"", "Garlic", "Bell Pepper", "Long Green Pepper", "Black Pepper", "Peppercorns",
		"Pepper Jack Cheese", "Pepperoni", "Sweet Thai Chili Sauce", "Smoked Paprika",
		"Sour Cream", "Tomato Paste", "Cumin", "Sweet Soy Glaze", "Banana Peppers",
	}
	for _, name := range mild {
		if Spicy(name) {
			t.Errorf("Spicy(%q) = true, want false", name)
		}
	}
}

// Heat is a classification of the catalog's own names, so it has to survive
// the same spellings Categorize does.
func TestSpicyIgnoresAccentsAndPunctuation(t *testing.T) {
	for _, name := range []string{"JALAPEÑO", "jalapeno", "Jalapeño & Lime", "chili-flakes"} {
		if !Spicy(name) {
			t.Errorf("Spicy(%q) = false, want true", name)
		}
	}
}
