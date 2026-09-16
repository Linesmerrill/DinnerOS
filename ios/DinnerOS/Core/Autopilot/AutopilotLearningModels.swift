import Foundation

/// What Autopilot learned from the household's feedback (`GET .../autopilot/learning`):
/// swaps, meals left out, accepted suggestions, views, cooks, skips, and ratings.
nonisolated struct AutopilotLearning: Decodable, Equatable, Sendable {
    let modelVersion: String
    /// How many feedback events were considered.
    let interactions: Int
    /// Strongest first.
    let adjustments: [AutopilotLearnedAdjustment]
    let resetAt: Date?
    let resetBy: String?
}

nonisolated struct AutopilotLearnedAdjustment: Decodable, Hashable, Sendable, Identifiable {
    nonisolated enum Kind: String, Decodable, Sendable {
        case item, busySkips, cuisine, protein, mealCategory, timeBand
        case unknown

        init(from decoder: Decoder) throws {
            self = Kind(rawValue: try decoder.singleValueContainer().decode(String.self)) ?? .unknown
        }
    }

    let kind: Kind
    let key: String
    /// The recipe or attribute it's about ("Beef Tacos", "Mexican").
    let label: String
    let recipeID: String?
    /// -1…1.
    let value: Double
    /// `toward` or `away`.
    let direction: String
    let text: String
    let evidence: Int

    var id: String { "\(kind.rawValue)/\(key)" }
    var isAway: Bool { direction == "away" }

    private enum CodingKeys: String, CodingKey {
        case kind, key, label
        case recipeID = "recipeId"
        case value, direction, text, evidence
    }
}
