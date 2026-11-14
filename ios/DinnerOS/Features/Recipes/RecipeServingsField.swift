import SwiftUI

/// How many a recipe serves: a stepper for small changes, and the number itself can be tapped
/// and typed for a batch of cookies that makes 36.
struct RecipeServingsField: View {
    @Binding var servings: Int

    var body: some View {
        HStack(spacing: 12) {
            Text("Serves")
            Spacer(minLength: 8)
            TextField("Not set", text: text)
                .keyboardType(.numberPad)
                .multilineTextAlignment(.center)
                .font(.body.monospacedDigit())
                .frame(width: 72, height: 40)
                .background(Color(.tertiarySystemFill), in: .rect(cornerRadius: 8))
                .accessibilityLabel("Servings")
            Stepper("Servings", value: $servings, in: RecipeServings.range)
                .labelsHidden()
        }
    }

    /// The typed number, kept to digits and to the most a recipe can serve.
    private var text: Binding<String> {
        Binding(
            get: { servings == 0 ? "" : String(servings) },
            set: { servings = RecipeServings.typed($0) }
        )
    }
}

enum RecipeServings {
    /// 0 means the recipe didn't say.
    static let range = 0...99

    /// What a typed servings number means: only its digits count, and it stops at the most allowed.
    static func typed(_ text: String) -> Int {
        let digits = text.filter(\.isWholeNumber).prefix(3)
        guard let value = Int(digits) else { return 0 }
        return min(value, range.upperBound)
    }
}
