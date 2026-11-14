import SwiftUI
import UIKit

/// One ingredient while a member types it: the name on its own line, then the amount and unit
/// below, each a full-size target. Return in the name moves to the amount; the amount keypad's
/// Next key moves on to the next ingredient, making one when this is the last.
struct IngredientEntryRow<ID: Hashable>: View {
    let id: ID
    @Binding var name: String
    @Binding var amount: String
    @Binding var unit: String
    let units: [String]
    var nameFocus: FocusState<ID?>.Binding
    @Binding var amountFocus: ID?
    /// Leaving the amount with Next. True when the cursor moved on to another ingredient;
    /// false closes the keypad.
    let next: () -> Bool

    var body: some View {
        VStack(alignment: .leading, spacing: 10) {
            TextField("Ingredient", text: $name)
                .font(.body.weight(.semibold))
                .textInputAutocapitalization(.sentences)
                .submitLabel(.next)
                .focused(nameFocus, equals: id)
                .onSubmit { amountFocus = id }
                .padding(.horizontal, 12)
                .frame(minHeight: 48)
                .background(Color(.tertiarySystemFill), in: .rect(cornerRadius: 10))
            HStack(spacing: 10) {
                AmountKeypadField(
                    text: $amount, unit: $unit, units: units,
                    isFocused: Binding(
                        get: { amountFocus == id },
                        set: { focused in
                            if focused {
                                amountFocus = id
                            } else if amountFocus == id {
                                amountFocus = nil
                            }
                        }),
                    next: next
                )
                .padding(.horizontal, 12)
                .frame(minHeight: 48)
                .background(Color(.tertiarySystemFill), in: .rect(cornerRadius: 10))
                .accessibilityLabel("Amount for \(name.isEmpty ? String(localized: "this ingredient") : name)")
                UnitMenu(unit: $unit, units: units)
            }
        }
        .padding(.vertical, 6)
    }
}

/// The unit as a button that opens every unit, for when the keypad isn't up.
struct UnitMenu: View {
    @Binding var unit: String
    let units: [String]

    var body: some View {
        Menu {
            Picker("Unit", selection: $unit) {
                ForEach(units, id: \.self) { code in
                    Text(RecipeUnit.label(code)).tag(code)
                }
            }
        } label: {
            HStack(spacing: 6) {
                Text(RecipeUnit.label(unit))
                    .lineLimit(1)
                Image(systemName: "chevron.up.chevron.down")
                    .font(.caption.weight(.semibold))
                    .accessibilityHidden(true)
            }
            .padding(.horizontal, 14)
            .frame(minWidth: 96, minHeight: 48)
            .background(Color(.tertiarySystemFill), in: .rect(cornerRadius: 10))
            .contentShape(.rect)
        }
        .accessibilityLabel("Unit")
        .accessibilityValue(RecipeUnit.label(unit))
    }
}

// MARK: - The amount field and its keypad

/// A text field whose keyboard is the amount keypad: digits, fractions, and the common units.
/// A hardware keyboard still types into it as usual.
struct AmountKeypadField: UIViewRepresentable {
    @Binding var text: String
    @Binding var unit: String
    let units: [String]
    @Binding var isFocused: Bool
    /// True when the cursor moved on to another field, which takes the keyboard over.
    let next: () -> Bool

    func makeCoordinator() -> Coordinator { Coordinator(self) }

    func makeUIView(context: Context) -> UITextField {
        let field = UITextField()
        field.placeholder = String(localized: "Amount")
        field.font = .preferredFont(forTextStyle: .body)
        field.adjustsFontForContentSizeCategory = true
        field.delegate = context.coordinator
        field.setContentHuggingPriority(.defaultLow, for: .horizontal)
        field.setContentCompressionResistancePriority(.defaultLow, for: .horizontal)
        field.addTarget(context.coordinator, action: #selector(Coordinator.edited(_:)), for: .editingChanged)
        field.inputView = context.coordinator.makeKeypad()
        field.inputAssistantItem.leadingBarButtonGroups = []
        field.inputAssistantItem.trailingBarButtonGroups = []
        context.coordinator.field = field
        return field
    }

    func updateUIView(_ field: UITextField, context: Context) {
        context.coordinator.parent = self
        context.coordinator.model.unit = unit
        context.coordinator.model.units = units
        let shown = AmountEntry.display(text)
        if field.text != shown { field.text = shown }
        // Only ever takes the keyboard here. Giving it up is left to whichever field takes it
        // next, or to the keypad's own buttons: resigning here as well raced the next field and
        // left it with a cursor that ignored typing.
        if isFocused, !field.isFirstResponder {
            DispatchQueue.main.async { if self.isFocused { field.becomeFirstResponder() } }
        }
    }

    final class Coordinator: NSObject, UITextFieldDelegate {
        var parent: AmountKeypadField
        weak var field: UITextField?
        let model = AmountKeypadModel()

        init(_ parent: AmountKeypadField) {
            self.parent = parent
            super.init()
            model.unit = parent.unit
            model.units = parent.units
            model.press = { [weak self] key in self?.press(key) }
            model.pick = { [weak self] code in
                self?.model.unit = code
                self?.parent.unit = code
            }
            model.next = { [weak self] in self?.moveOn() }
            model.close = { [weak self] in self?.field?.resignFirstResponder() }
        }

        func makeKeypad() -> UIView {
            let host = UIHostingController(rootView: AmountKeypad(model: model))
            host.view.backgroundColor = .clear
            let container = UIInputView(frame: CGRect(x: 0, y: 0, width: 0, height: 340), inputViewStyle: .keyboard)
            container.allowsSelfSizing = true
            host.view.translatesAutoresizingMaskIntoConstraints = false
            container.addSubview(host.view)
            NSLayoutConstraint.activate([
                host.view.leadingAnchor.constraint(equalTo: container.leadingAnchor),
                host.view.trailingAnchor.constraint(equalTo: container.trailingAnchor),
                host.view.topAnchor.constraint(equalTo: container.topAnchor),
                host.view.bottomAnchor.constraint(equalTo: container.safeAreaLayoutGuide.bottomAnchor),
            ])
            hostingController = host
            return container
        }

        private var hostingController: UIHostingController<AmountKeypad>?

        private func press(_ key: AmountEntry.Key) {
            let updated = AmountEntry.apply(key, to: parent.text)
            guard updated != parent.text else {
                UIDevice.current.playInputClick()
                return
            }
            parent.text = updated
            field?.text = AmountEntry.display(updated)
            UIDevice.current.playInputClick()
        }

        @objc func edited(_ field: UITextField) {
            parent.text = AmountEntry.raw(field.text ?? "")
        }

        func textFieldDidBeginEditing(_ textField: UITextField) {
            if !parent.isFocused { parent.isFocused = true }
        }

        func textFieldDidEndEditing(_ textField: UITextField) {
            if parent.isFocused { parent.isFocused = false }
        }

        func textFieldShouldReturn(_ textField: UITextField) -> Bool {
            moveOn()
            return false
        }

        /// Next: on to the next ingredient, or the keypad closes.
        private func moveOn() {
            if !parent.next() { field?.resignFirstResponder() }
        }
    }
}

/// What the keypad shows and does. Kept apart from the field so the keypad redraws when
/// the unit changes.
@Observable
final class AmountKeypadModel {
    var unit = "count"
    var units: [String] = RecipeUnit.codes
    @ObservationIgnored var press: (AmountEntry.Key) -> Void = { _ in }
    @ObservationIgnored var pick: (String) -> Void = { _ in }
    @ObservationIgnored var next: () -> Void = {}
    @ObservationIgnored var close: () -> Void = {}
}

/// The amount keypad: common units as chips, then fractions, then digits. Next moves on to the
/// next ingredient.
struct AmountKeypad: View {
    let model: AmountKeypadModel

    @Environment(\.horizontalSizeClass) private var sizeClass

    var body: some View {
        VStack(spacing: 8) {
            units
            HStack(spacing: 6) {
                ForEach(Array(AmountEntry.fractions.enumerated()), id: \.offset) { _, fraction in
                    key(
                        AmountEntry.symbol(fraction.0, fraction.1),
                        spoken: spokenFraction(fraction.0, fraction.1)
                    ) { model.press(.fraction(fraction.0, fraction.1)) }
                }
            }
            .frame(height: 44)
            HStack(spacing: 6) {
                VStack(spacing: 6) {
                    digitRow([1, 2, 3])
                    digitRow([4, 5, 6])
                    digitRow([7, 8, 9])
                    HStack(spacing: 6) {
                        key(".", spoken: String(localized: "Point")) { model.press(.decimal) }
                        key("0", spoken: "0") { model.press(.digit(0)) }
                        key(systemImage: "delete.left", spoken: String(localized: "Delete")) {
                            model.press(.delete)
                        }
                    }
                }
                VStack(spacing: 6) {
                    key(systemImage: "keyboard.chevron.compact.down", spoken: String(localized: "Hide keypad")) {
                        model.close()
                    }
                    Button {
                        model.next()
                    } label: {
                        VStack(spacing: 4) {
                            Image(systemName: "return")
                                .accessibilityHidden(true)
                            Text("Next")
                                .font(.headline)
                        }
                        .frame(maxWidth: .infinity, maxHeight: .infinity)
                        .foregroundStyle(.white)
                        .background(Color.accentColor, in: .rect(cornerRadius: 8))
                        .contentShape(.rect)
                    }
                    .buttonStyle(KeypadButtonStyle())
                    .accessibilityLabel("Next ingredient")
                    .frame(maxHeight: .infinity)
                    .layoutPriority(3)
                }
                .frame(maxWidth: 110)
            }
        }
        .dynamicTypeSize(...DynamicTypeSize.xxxLarge)
        .padding(.horizontal, 8)
        .padding(.top, 8)
        .padding(.bottom, 4)
        .frame(maxWidth: sizeClass == .regular ? 560 : .infinity)
        .frame(maxWidth: .infinity)
    }

    private var units: some View {
        ScrollView(.horizontal, showsIndicators: false) {
            HStack(spacing: 6) {
                ForEach(RecipeUnit.quick.filter(model.units.contains), id: \.self) { code in
                    chip(code)
                }
                if !RecipeUnit.quick.contains(model.unit), model.units.contains(model.unit) {
                    chip(model.unit)
                }
                Menu {
                    Picker("Unit", selection: Binding(get: { model.unit }, set: { model.pick($0) })) {
                        ForEach(model.units, id: \.self) { code in
                            Text(RecipeUnit.label(code)).tag(code)
                        }
                    }
                } label: {
                    Label("More", systemImage: "ellipsis")
                        .labelStyle(.titleAndIcon)
                        .font(.subheadline.weight(.semibold))
                        .padding(.horizontal, 14)
                        .frame(minHeight: 40)
                        .background(Color(.systemBackground), in: .capsule)
                }
                .accessibilityLabel("More units")
            }
            .padding(.horizontal, 2)
        }
        .frame(height: 44)
    }

    private func chip(_ code: String) -> some View {
        let selected = model.unit == code
        return Button {
            model.pick(code)
        } label: {
            Text(RecipeUnit.label(code))
                .font(.subheadline.weight(.semibold))
                .padding(.horizontal, 14)
                .frame(minHeight: 40)
                .foregroundStyle(selected ? Color.white : Color.primary)
                .background(selected ? Color.accentColor : Color(.systemBackground), in: .capsule)
                .contentShape(.capsule)
        }
        .buttonStyle(KeypadButtonStyle())
        .accessibilityLabel(RecipeUnit.label(code))
        .accessibilityAddTraits(selected ? .isSelected : [])
    }

    private func digitRow(_ digits: [Int]) -> some View {
        HStack(spacing: 6) {
            ForEach(digits, id: \.self) { digit in
                key("\(digit)", spoken: "\(digit)") { model.press(.digit(digit)) }
            }
        }
    }

    private func key(_ title: String, spoken: String, action: @escaping () -> Void) -> some View {
        Button(action: action) {
            Text(title)
                .font(.title2)
                .frame(maxWidth: .infinity, maxHeight: .infinity)
                .foregroundStyle(Color.primary)
                .background(Color(.systemBackground), in: .rect(cornerRadius: 8))
                .contentShape(.rect)
        }
        .buttonStyle(KeypadButtonStyle())
        .accessibilityLabel(spoken)
        .frame(minHeight: 44)
    }

    private func key(systemImage: String, spoken: String, action: @escaping () -> Void) -> some View {
        Button(action: action) {
            Image(systemName: systemImage)
                .font(.title3)
                .frame(maxWidth: .infinity, maxHeight: .infinity)
                .foregroundStyle(Color.primary)
                .background(Color(.systemBackground).opacity(0.6), in: .rect(cornerRadius: 8))
                .contentShape(.rect)
        }
        .buttonStyle(KeypadButtonStyle())
        .accessibilityLabel(spoken)
        .frame(minHeight: 44)
    }

    private func spokenFraction(_ n: Int, _ d: Int) -> String {
        switch (n, d) {
        case (1, 4): String(localized: "One quarter")
        case (1, 3): String(localized: "One third")
        case (1, 2): String(localized: "One half")
        case (2, 3): String(localized: "Two thirds")
        case (3, 4): String(localized: "Three quarters")
        default: "\(n)/\(d)"
        }
    }
}

/// A key dims while pressed, like the system keyboard's.
private struct KeypadButtonStyle: ButtonStyle {
    func makeBody(configuration: Configuration) -> some View {
        configuration.label
            .opacity(configuration.isPressed ? 0.55 : 1)
    }
}

extension UIInputView: @retroactive UIInputViewAudioFeedback {
    public var enableInputClicksWhenVisible: Bool { true }
}
