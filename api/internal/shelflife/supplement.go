package shelflife

// supplement fills gaps in FoodKeeper for things households buy every week:
// foods it lists without a time for where they're usually kept (milk, sour
// cream, tomatoes on the counter), or by a name it doesn't use. The times
// follow FoodSafety.gov's refrigerator and pantry guidance, on the short side.
// They're checked before the seed, so they win a tie.
var supplement = []Entry{
	{ID: 100001, Category: "Dairy Products & Eggs", Name: "Milk", Fridge: &Range{7, 7}, Freezer: &Range{90, 90}},
	{ID: 100002, Category: "Dairy Products & Eggs", Name: "Sour cream", Fridge: &Range{7, 21}},
	{ID: 100003, Category: "Produce · Fresh Vegetables", Name: "Tomatoes", Pantry: &Range{3, 7}, Fridge: &Range{7, 14}, Freezer: &Range{60, 60}},
	{ID: 100004, Category: "Produce · Fresh Fruits", Name: "Avocados", Pantry: &Range{3, 5}, Fridge: &Range{3, 5}},
	{ID: 100005, Category: "Produce · Fresh Vegetables", Name: "Green onions", Keywords: []string{"scallion", "spring onion"}, Fridge: &Range{7, 14}, Freezer: &Range{300, 360}},
	{ID: 100006, Category: "Produce · Fresh Fruits", Name: "Limes", Keywords: []string{"lime"}, Pantry: &Range{7, 10}, Fridge: &Range{14, 21}},
	{ID: 100007, Category: "Produce · Fresh Fruits", Name: "Lemons", Keywords: []string{"lemon"}, Pantry: &Range{7, 10}, Fridge: &Range{14, 21}},
	{ID: 100008, Category: "Produce · Fresh Vegetables", Name: "Spinach", Fridge: &Range{3, 7}, Freezer: &Range{300, 360}},
	{ID: 100009, Category: "Seafood · Fresh", Name: "Salmon", Keywords: []string{"fillet", "filet"}, Fridge: &Range{1, 2}, Freezer: &Range{60, 90}},
	{ID: 100010, Category: "Dairy Products & Eggs", Name: "Mozzarella", Keywords: []string{"cheese"}, Fridge: &Range{7, 14}, Freezer: &Range{90, 180}},
}

const supplementSource = "DinnerOS, from FoodSafety.gov storage guidance"

func init() {
	for i := range supplement {
		supplement[i].Source = supplementSource
	}
	seed = append(append([]Entry(nil), supplement...), seed...)
}
