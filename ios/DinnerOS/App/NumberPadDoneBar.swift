import UIKit

/// Number pads have no return key, so a field typed on one could never be put away. Every text
/// field that opens one gets a Done bar above it, app-wide, so no screen has to remember — and a
/// screen with a price on every row doesn't stack one bar per row.
@MainActor
enum NumberPadDoneBar {
    private static var observer: NSObjectProtocol?

    static let keyboards: Set<UIKeyboardType> = [.numberPad, .decimalPad, .phonePad, .asciiCapableNumberPad]

    static func install() {
        guard observer == nil else { return }
        observer = NotificationCenter.default.addObserver(
            forName: UITextField.textDidBeginEditingNotification, object: nil, queue: .main
        ) { note in
            guard let field = note.object as? UITextField else { return }
            MainActor.assumeIsolated { attach(to: field) }
        }
    }

    static func attach(to field: UITextField) {
        guard keyboards.contains(field.keyboardType), field.inputAccessoryView == nil else { return }
        let bar = UIToolbar(frame: CGRect(x: 0, y: 0, width: 320, height: 44))
        let done = UIBarButtonItem(
            systemItem: .done,
            primaryAction: UIAction { [weak field] _ in field?.resignFirstResponder() })
        bar.items = [UIBarButtonItem(systemItem: .flexibleSpace), done]
        bar.sizeToFit()
        field.inputAccessoryView = bar
        field.reloadInputViews()
    }
}
