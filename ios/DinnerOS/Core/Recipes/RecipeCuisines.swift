import Foundation

/// The cuisines a recipe can have: the autopilot vocabulary's list, which always holds the
/// standard cuisines and the household's own, and a guess from a title while one is typed.
/// The server owns both; the bundled list stands in until it answers, or offline with nothing
/// saved (decisions 721 to 723).
nonisolated enum RecipeCuisines {
    /// The standard cuisines, as the server sends them, for an API that hasn't answered yet.
    static let standard: [AutopilotOption] = [
        ("brazilian", "Brazilian"), ("british", "British"), ("cajun", "Cajun"), ("chinese", "Chinese"),
        ("cuban", "Cuban"), ("ethiopian", "Ethiopian"), ("filipino", "Filipino"), ("french", "French"),
        ("german", "German"), ("greek", "Greek"), ("hawaiian", "Hawaiian"), ("hungarian", "Hungarian"),
        ("indian", "Indian"), ("indonesian", "Indonesian"), ("irish", "Irish"), ("italian", "Italian"),
        ("jamaican", "Jamaican"), ("japanese", "Japanese"), ("korean", "Korean"), ("lebanese", "Lebanese"),
        ("mediterranean", "Mediterranean"), ("mexican", "Mexican"), ("middle eastern", "Middle Eastern"),
        ("moroccan", "Moroccan"), ("persian", "Persian"), ("peruvian", "Peruvian"), ("polish", "Polish"),
        ("portuguese", "Portuguese"), ("russian", "Russian"), ("southern", "Southern"),
        ("southwestern", "Southwestern"), ("spanish", "Spanish"), ("tex-mex", "Tex-Mex"), ("thai", "Thai"),
        ("turkish", "Turkish"), ("vietnamese", "Vietnamese"),
    ].map { AutopilotOption(value: $0.0, label: $0.1, description: nil, recipeCount: nil) }

    /// How many cuisines a recipe keeps.
    static let maxPerRecipe = 5

    /// A value's label from `options`, else the standard list, else the value capitalized.
    static func label(for value: String, in options: [AutopilotOption]) -> String {
        if let option = options.first(where: { $0.value == value }) ?? standard.first(where: { $0.value == value }) {
            return option.label
        }
        return value.capitalized
    }

    /// What the Cuisine row says: "None", "Mexican", or "Mexican and Italian".
    static func summary(_ values: [String], in options: [AutopilotOption]) -> String {
        guard !values.isEmpty else { return String(localized: "None") }
        return values.map { label(for: $0, in: options) }.formatted(.list(type: .and))
    }

    /// The list to pick from: `options`, with any of `current` it doesn't have first, so a
    /// recipe's own cuisine can always be seen and kept.
    static func choices(_ options: [AutopilotOption], keeping current: [String]) -> [AutopilotOption] {
        let list = options.isEmpty ? standard : options
        let missing = current.filter { value in !list.contains { $0.value == value } }
            .map { AutopilotOption(value: $0, label: label(for: $0, in: list), description: nil, recipeCount: nil) }
        return missing + list
    }

    /// Options whose label or value contains `query`, ignoring case and accents.
    static func filter(_ options: [AutopilotOption], query: String) -> [AutopilotOption] {
        let query = query.trimmingCharacters(in: .whitespaces)
        guard !query.isEmpty else { return options }
        return options.filter {
            $0.label.localizedStandardContains(query) || $0.value.localizedStandardContains(query)
        }
    }
}

/// The vocabulary's cuisines, read from the autopilot vocabulary response.
nonisolated struct RecipeCuisineVocabulary: Decodable, Equatable, Sendable {
    let cuisines: [AutopilotOption]
}

/// Body and answer of `POST .../recipes/cuisine-guess`.
nonisolated struct RecipeCuisineGuessRequest: Encodable, Equatable, Sendable {
    let name: String
}

nonisolated struct RecipeCuisineGuess: Decodable, Equatable, Sendable {
    let cuisines: [String]
}

nonisolated extension RecipesAPI {
    /// The cuisines to offer on a recipe. The same request as Autopilot's vocabulary, so the
    /// answer saved for offline use is shared.
    func cuisineOptions(householdID: String, accessToken: String) async throws -> [AutopilotOption] {
        let vocabulary: RecipeCuisineVocabulary = try await client.send(
            APIRequest.get("/api/v1/households/\(householdID)/autopilot/vocabulary").authorized(with: accessToken))
        return vocabulary.cuisines
    }

    /// The cuisine a title names. Nothing is stored.
    func guessCuisine(householdID: String, name: String, accessToken: String) async throws -> [String] {
        let request = try APIRequest.post(
            "/api/v1/households/\(householdID)/recipes/cuisine-guess", body: RecipeCuisineGuessRequest(name: name))
        let guess: RecipeCuisineGuess = try await client.send(request.authorized(with: accessToken))
        return guess.cuisines
    }
}
