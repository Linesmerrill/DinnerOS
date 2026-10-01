import Testing
import UIKit

@testable import DinnerOS

/// A number pad has no return key, so its field gets a Done bar; other keyboards are left alone.
@MainActor
struct NumberPadDoneBarTests {
    @Test func aNumberPadGetsADoneBar() throws {
        for keyboard in [UIKeyboardType.decimalPad, .numberPad] {
            let field = UITextField()
            field.keyboardType = keyboard
            NumberPadDoneBar.attach(to: field)
            let bar = try #require(field.inputAccessoryView as? UIToolbar)
            #expect(bar.items?.count == 2)
        }
    }

    @Test func otherKeyboardsAndExistingBarsAreLeftAlone() {
        let text = UITextField()
        NumberPadDoneBar.attach(to: text)
        #expect(text.inputAccessoryView == nil)

        let custom = UIView()
        let field = UITextField()
        field.keyboardType = .decimalPad
        field.inputAccessoryView = custom
        NumberPadDoneBar.attach(to: field)
        #expect(field.inputAccessoryView === custom)
    }
}
