import SwiftUI
import UIKit

extension View {
    /// Every way to put the keyboard away on a long form: drag the form down, tap outside the
    /// field, or tap Done above the keyboard. `showsDone: false` leaves Done out: an iPad sheet's
    /// Done floated beside the sheet, and the iPad keyboard has its own hide key (build 122).
    func keyboardDismissal(showsDone: Bool = true) -> some View {
        modifier(KeyboardDismissal(showsDone: showsDone))
    }
}

private struct KeyboardDismissal: ViewModifier {
    let showsDone: Bool

    func body(content: Content) -> some View {
        content
            .scrollDismissesKeyboard(.interactively)
            .background { TapOutsideToDismiss() }
            .toolbar {
                if showsDone {
                    ToolbarItemGroup(placement: .keyboard) {
                        Spacer()
                        Button("Done") { Keyboard.dismiss() }
                            .fontWeight(.semibold)
                    }
                }
            }
    }
}

enum Keyboard {
    /// Puts away whatever keyboard is up, SwiftUI or UIKit.
    @MainActor static func dismiss() {
        UIApplication.shared.sendAction(#selector(UIResponder.resignFirstResponder), to: nil, from: nil, for: nil)
    }
}

/// Watches taps anywhere in the window while the form is on screen, and puts the keyboard away
/// when one lands outside a text field. It never takes the tap from what's under it, so buttons,
/// menus and rows still work.
private struct TapOutsideToDismiss: UIViewRepresentable {
    func makeCoordinator() -> Coordinator { Coordinator() }

    func makeUIView(context: Context) -> WindowWatcher {
        let view = WindowWatcher()
        view.coordinator = context.coordinator
        view.isUserInteractionEnabled = false
        return view
    }

    func updateUIView(_ view: WindowWatcher, context: Context) {}

    static func dismantleUIView(_ view: WindowWatcher, coordinator: Coordinator) {
        coordinator.detach()
    }

    final class WindowWatcher: UIView {
        weak var coordinator: Coordinator?

        override func didMoveToWindow() {
            super.didMoveToWindow()
            coordinator?.attach(to: window)
        }
    }

    final class Coordinator: NSObject, UIGestureRecognizerDelegate {
        private lazy var tap: UITapGestureRecognizer = {
            let tap = UITapGestureRecognizer(target: self, action: #selector(tapped))
            tap.cancelsTouchesInView = false
            tap.delegate = self
            return tap
        }()

        func attach(to window: UIWindow?) {
            detach()
            window?.addGestureRecognizer(tap)
        }

        func detach() {
            tap.view?.removeGestureRecognizer(tap)
        }

        @objc private func tapped() {
            tap.view?.endEditing(true)
        }

        func gestureRecognizer(_ recognizer: UIGestureRecognizer, shouldReceive touch: UITouch) -> Bool {
            !Self.isInTextField(touch.view)
        }

        func gestureRecognizer(
            _ recognizer: UIGestureRecognizer,
            shouldRecognizeSimultaneouslyWith other: UIGestureRecognizer
        ) -> Bool { true }

        /// A tap in a field moves the cursor or starts typing there, so it keeps the keyboard.
        static func isInTextField(_ view: UIView?) -> Bool {
            var view = view
            while let current = view {
                if current is UITextField || current is UITextView { return true }
                view = current.superview
            }
            return false
        }
    }
}
