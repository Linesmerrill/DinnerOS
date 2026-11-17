import SwiftUI

/// A stand-in until companions ship: the Add a Recipe choices open this for Add a Companion.
/// The companions work replaces this file with the real editor, keeping this initializer.
struct CompanionEditorView: View {
    let householdID: String
    var onSaved: () -> Void = {}

    var body: some View {
        ContentUnavailableView(
            "Companions Are Coming", systemImage: "takeoutbag.and.cup.and.straw",
            description: Text("Soon you can add the things that go with your dinners, like bread or a side salad."))
            .navigationTitle("Add a Companion")
            .navigationBarTitleDisplayMode(.inline)
            .accessibilityIdentifier("CompanionEditor")
    }
}
