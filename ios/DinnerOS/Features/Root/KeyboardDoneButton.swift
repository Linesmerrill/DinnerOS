import SwiftUI

/// A Done button above the keyboard while this field has focus. Number and decimal pads have
/// no Return key, so without it the keyboard can't be put away.
private struct KeyboardDoneButton: ViewModifier {
    @FocusState private var isFocused: Bool

    func body(content: Content) -> some View {
        content
            .focused($isFocused)
            .toolbar {
                if isFocused {
                    ToolbarItemGroup(placement: .keyboard) {
                        Spacer()
                        Button("Done") { isFocused = false }
                    }
                }
            }
    }
}

extension View {
    /// Adds a Done button above the keyboard while this field is being edited. Use it on fields
    /// with a number or decimal pad that don't already track focus.
    func keyboardDoneButton() -> some View {
        modifier(KeyboardDoneButton())
    }
}
