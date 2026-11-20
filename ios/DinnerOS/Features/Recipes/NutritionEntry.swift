import SwiftUI

/// A nutrition value a member can type, per serving, with the name and unit the server uses.
nonisolated enum NutritionField: String, CaseIterable, Identifiable, Sendable {
    case calories, protein, carbohydrate, fat, fiber, sugar, sodium, saturatedFat

    var id: Self { self }

    /// The four most people look for. The rest are under More.
    static let main: [NutritionField] = [.calories, .protein, .carbohydrate, .fat]
    static let more: [NutritionField] = [.fiber, .sugar, .sodium, .saturatedFat]

    /// The name as the server stores it, the same as imports.
    var serverName: String {
        switch self {
        case .calories: "Calories"
        case .protein: "Protein"
        case .carbohydrate: "Carbohydrate"
        case .fat: "Fat"
        case .fiber: "Dietary Fiber"
        case .sugar: "Sugar"
        case .sodium: "Sodium"
        case .saturatedFat: "Saturated Fat"
        }
    }

    var label: LocalizedStringKey {
        switch self {
        case .calories: "Calories"
        case .protein: "Protein"
        case .carbohydrate: "Carbs"
        case .fat: "Fat"
        case .fiber: "Fiber"
        case .sugar: "Sugar"
        case .sodium: "Sodium"
        case .saturatedFat: "Saturated Fat"
        }
    }

    var unit: String {
        switch self {
        case .calories: "kcal"
        case .sodium: "mg"
        default: "g"
        }
    }

    /// The server's name, however an import spelled it.
    static func matching(_ name: String) -> NutritionField? {
        let key = name.lowercased().filter(\.isLetter)
        switch key {
        case "calories", "energy", "kcal": return .calories
        case "protein": return .protein
        case "carbohydrate", "carbohydrates", "carbs", "totalcarbohydrate": return .carbohydrate
        case "fat", "totalfat": return .fat
        case "dietaryfiber", "fiber", "fibre": return .fiber
        case "sugar", "sugars", "totalsugars": return .sugar
        case "sodium": return .sodium
        case "saturatedfat", "saturated": return .saturatedFat
        default: return nil
        }
    }
}

/// One value sent to the server: `{"name":"Protein","amount":30,"unit":"g"}`.
nonisolated struct RecipeNutrientEntry: Codable, Equatable, Sendable {
    let name: String
    let amount: Double
    let unit: String
}

/// Nutrition as typed, per serving: text by field, so "12." can be typed on the way to "12.5".
nonisolated struct NutritionValues: Equatable, Sendable {
    var text: [NutritionField: String] = [:]

    init(text: [NutritionField: String] = [:]) {
        self.text = text
    }

    init(_ nutrients: [RecipeNutrient]) {
        for nutrient in nutrients {
            guard let field = NutritionField.matching(nutrient.name) else { continue }
            text[field] = Self.format(nutrient.amount)
        }
    }

    init(_ entries: [RecipeNutrientEntry]) {
        self.init(entries.map { RecipeNutrient(name: $0.name, amount: $0.amount, unit: $0.unit) })
    }

    subscript(field: NutritionField) -> String {
        get { text[field] ?? "" }
        set { text[field] = newValue }
    }

    var isEmpty: Bool { entries.isEmpty && problem == nil }

    /// What can't be saved, in plain words; `nil` when it can.
    var problem: String? {
        for field in NutritionField.allCases {
            let value = self[field].trimmingCharacters(in: .whitespaces)
            if !value.isEmpty, Self.number(value) == nil {
                return String(localized: "Enter a number for \(String(localized: field.labelText)).")
            }
        }
        return nil
    }

    /// What's sent: only the values that are filled in.
    var entries: [RecipeNutrientEntry] {
        NutritionField.allCases.compactMap { field in
            Self.number(self[field]).map { RecipeNutrientEntry(name: field.serverName, amount: $0, unit: field.unit) }
        }
    }

    static func number(_ text: String) -> Double? {
        let cleaned = text.trimmingCharacters(in: .whitespaces).replacingOccurrences(of: ",", with: ".")
        guard !cleaned.isEmpty, let value = Double(cleaned), value >= 0, value.isFinite else { return nil }
        return value
    }

    static func format(_ value: Double) -> String {
        value == value.rounded() ? String(Int(value)) : String(format: "%.1f", value)
    }
}

nonisolated extension NutritionField {
    /// The label as plain text, for sentences.
    var labelText: String.LocalizationValue {
        switch self {
        case .calories: "calories"
        case .protein: "protein"
        case .carbohydrate: "carbs"
        case .fat: "fat"
        case .fiber: "fiber"
        case .sugar: "sugar"
        case .sodium: "sodium"
        case .saturatedFat: "saturated fat"
        }
    }
}

/// The Nutrition section of a recipe form: calories, protein, carbs and fat, and More for the
/// rest. Each on a number keypad with its unit beside it.
struct NutritionSection: View {
    @Binding var values: NutritionValues
    @State private var showsMore = false

    var body: some View {
        Section {
            ForEach(NutritionField.main) { field in
                NutritionRow(field: field, text: $values[field])
            }
            if showsMore || NutritionField.more.contains(where: { !values[$0].isEmpty }) {
                ForEach(NutritionField.more) { field in
                    NutritionRow(field: field, text: $values[field])
                }
            } else {
                Button {
                    withAnimation { showsMore = true }
                } label: {
                    Label("More Nutrition", systemImage: "plus")
                        .frame(minHeight: DS.Size.tapTarget)
                }
            }
            if let problem = values.problem {
                Label(problem, systemImage: "exclamationmark.triangle")
                    .font(.footnote)
                    .foregroundStyle(.secondary)
            }
        } header: {
            Text("Nutrition per Serving")
        } footer: {
            Text("Optional. Fill in what you know.")
        }
    }
}

private struct NutritionRow: View {
    let field: NutritionField
    @Binding var text: String

    var body: some View {
        HStack(spacing: DS.Space.s) {
            Text(field.label)
            Spacer(minLength: 8)
            TextField("Not set", text: $text)
                .keyboardType(field == .calories || field == .sodium ? .numberPad : .decimalPad)
                .multilineTextAlignment(.trailing)
                .monospacedDigit()
                .frame(maxWidth: 110, minHeight: 40)
                .accessibilityLabel(Text(field.label))
            Text(field.unit)
                .foregroundStyle(.secondary)
                .frame(minWidth: 34, alignment: .leading)
                .accessibilityHidden(true)
        }
    }
}

/// Add Nutrition from the recipe page's empty Nutrition tab.
struct NutritionEditorSheet: View {
    let recipe: Recipe
    let onSaved: (Recipe) -> Void

    @Environment(RecipeLibrary.self) private var library
    @Environment(\.dismiss) private var dismiss
    @State private var values: NutritionValues
    @State private var isSaving = false
    @State private var errorMessage: String?

    init(recipe: Recipe, onSaved: @escaping (Recipe) -> Void) {
        self.recipe = recipe
        self.onSaved = onSaved
        _values = State(initialValue: NutritionValues(recipe.nutritionValues))
    }

    var body: some View {
        NavigationStack {
            Form {
                NutritionSection(values: $values)
                if let errorMessage {
                    Label(errorMessage, systemImage: "exclamationmark.triangle")
                        .font(.footnote)
                        .foregroundStyle(.secondary)
                }
            }
            .keyboardDismissal()
            .navigationTitle("Nutrition")
            .navigationBarTitleDisplayMode(.inline)
            .toolbar {
                ToolbarItem(placement: .cancellationAction) {
                    Button("Cancel") { dismiss() }
                }
                ToolbarItem(placement: .confirmationAction) {
                    if isSaving {
                        ProgressView()
                    } else {
                        Button("Save") { save() }
                            .disabled(values.problem != nil)
                    }
                }
            }
        }
    }

    private func save() {
        Keyboard.dismiss()
        isSaving = true
        errorMessage = nil
        Task {
            defer { isSaving = false }
            do {
                let saved = try await library.editRecipe(recipeID: recipe.id, request: .nutrition(values.entries))
                onSaved(saved)
                dismiss()
            } catch is CancellationError {
                return
            } catch {
                errorMessage = HouseholdStore.message(for: error)
            }
        }
    }
}
