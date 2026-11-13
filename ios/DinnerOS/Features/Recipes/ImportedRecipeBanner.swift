import SwiftUI

/// On a recipe read from a web page: the reminder to check its ingredients, and a way to
/// the page itself for the steps and photos, which aren't copied.
struct ImportedRecipeBanner: View {
    let url: URL
    /// Read from a page, so the reminder applies; false shows only the button.
    var showsNote = true

    var body: some View {
        VStack(alignment: .leading, spacing: 10) {
            if showsNote {
                Label(ImportedRecipeText.note, systemImage: "checklist")
                    .font(.subheadline)
                    .foregroundStyle(.secondary)
            }
            Link(destination: url) {
                Label("View Recipe", systemImage: "safari")
                    .frame(maxWidth: .infinity)
            }
            .buttonStyle(.bordered)
            .controlSize(.large)
            .accessibilityHint("Opens the original recipe page for the steps and photos.")
        }
    }
}

#Preview {
    ImportedRecipeBanner(url: URL(string: "https://example.com/recipe").unsafelyUnwrapped)
        .padding()
}
