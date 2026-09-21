import Foundation

/// Synthetic catalog recipes for `#Preview` and for the redacted placeholder
/// rows the browse list shows while its first page loads.
///
/// Like `RecipePreviewData`, this is deliberately not `#if DEBUG`: the
/// placeholder rows ship, and a release build needs their shape.
enum CatalogPreviewData {
    /// A row shown redacted while the first page loads. Its text only has to be
    /// the right length; nobody reads it.
    static let placeholder = CatalogSummary(
        id: "catalog-placeholder", catalogKey: "hellofresh:placeholder", source: "hellofresh",
        name: "Harissa Chicken Bowls", headline: "With couscous and herbed yogurt", imageURLString: nil,
        isAddon: false, totalMinutes: 30, cookMinutes: 30, timeBand: nil, calories: nil, proteinGrams: nil,
        cuisines: ["Moroccan"], tags: ["Quick"], inLibrary: false, libraryRecipeID: nil, reasons: [])

    static let summaries: [CatalogSummary] = [
        CatalogSummary(
            id: "catalog-1", catalogKey: "hellofresh:1", source: "hellofresh", name: "Thai Green Curry",
            headline: "With jasmine rice", imageURLString: nil, isAddon: false, totalMinutes: 35, cookMinutes: 35,
            timeBand: nil, calories: 640, proteinGrams: 32, cuisines: ["Thai"], tags: ["Spicy"], inLibrary: false,
            libraryRecipeID: nil, reasons: ["You like Thai"]),
        CatalogSummary(
            id: "catalog-2", catalogKey: "hellofresh:2", source: "hellofresh", name: "Sheet-Pan Gnocchi",
            headline: "Crispy and fast", imageURLString: nil, isAddon: false, totalMinutes: 25, cookMinutes: 25,
            timeBand: nil, calories: 720, proteinGrams: 21, cuisines: ["Italian"], tags: ["Quick"], inLibrary: true,
            libraryRecipeID: "recipe-9", reasons: []),
    ]
}
