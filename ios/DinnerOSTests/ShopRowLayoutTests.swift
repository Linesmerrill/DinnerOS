import SwiftUI
import Testing
import UIKit

@testable import DinnerOS

/// Where a Shop row's Choose Product button goes. On an iPad the Lemon row's note ("1 for this
/// meal · Also in Chicken Penne al Limone") pushed the button under the text, out of line with
/// every other row (Hark's review, build 123). The note wraps beside the button now; on a row
/// too narrow for that (an iPhone) the note goes under the name and the button stays beside it.
///
/// Measured from the real view's height: beside the text the row is as tall as its text; under
/// it, the button's height is added.
@MainActor
struct ShopRowLayoutTests {
    private static let longNote =
        "1 for this meal · Also in Street Cart-Style Chicken Bowls and Chicken Penne al Limone"

    private func line(_ name: String) -> ShoppingExcludedLine {
        ShoppingExcludedLine(
            ingredientKey: "name:\(name.lowercased())", ingredientID: nil, name: name, category: "produce",
            amounts: [], quantityText: "2", unquantified: false, groceryStatus: .toBuy, reason: .noProduct,
            text: "Choose a product", searchTerms: ShoppingSearchTerms.plain(name))
    }

    private func height(of view: some View, width: CGFloat) -> CGFloat {
        let controller = UIHostingController(
            rootView: AnyView(view.environment(\.dynamicTypeSize, .large).frame(width: width)))
        return controller.sizeThatFits(in: CGSize(width: width, height: .greatestFiniteMagnitude)).height
    }

    private func row(note: String?, name: String = "Lemon") -> some View {
        NeedsProductRow(
            line: line(name), canChoose: true, mealNote: note,
            remove: ShopRemoveOptions(name: name, thisWeek: {}, always: {}), choose: {})
    }

    @Test func aLongNoteWrapsBesideTheButtonOnAnIPad() {
        // An iPad's readable column, landscape or portrait.
        let width: CGFloat = 560
        let plain = height(of: row(note: nil), width: width)
        let oneLineNote = height(of: row(note: "1 for this meal · Also in Tacos"), width: width)
        let long = height(of: row(note: Self.longNote), width: width)
        let button = height(of: Button("Choose Product") {}.buttonStyle(.dsSecondary(.small)), width: width)

        // The short note sits beside the button, so the row is only a line taller than without it.
        #expect(oneLineNote - plain < button, "plain \(plain), one-line note \(oneLineNote)")
        // The long note wraps to a second line beside the button instead of the button dropping
        // under the text: a line taller, not a button taller.
        #expect(long - oneLineNote < button, "one-line note \(oneLineNote), long note \(long), button \(button)")
    }

    @Test func onAPhoneTheButtonStaysBesideTheNameAndTheNoteGoesUnder() {
        // An iPhone SE's row. The button used to drop under the whole text there (Hark's review,
        // build 121); now it stays beside the name and the note runs the row's width under them,
        // so the row is no taller than its text plus a line or so for the narrower note.
        let wide = height(of: row(note: Self.longNote), width: 560)
        let button = height(of: Button("Choose Product") {}.buttonStyle(.dsSecondary(.small)), width: 288)
        for width: CGFloat in [288, 330, 360] {
            let narrow = height(of: row(note: Self.longNote), width: width)
            #expect(narrow - wide < button * 0.8, "at \(width): wide \(wide), narrow \(narrow), button \(button)")
        }
    }

    @Test func onAPhoneALongNameWrapsBesideTheButton() {
        // A name too long for one line beside the buttons on an iPhone ("Chicken Breast Strips",
        // build 121) wraps beside them instead of the buttons going under it. Two words here, so
        // a wrapped name (a line taller) and buttons under it (a button taller) differ in height.
        let name = "Ground Turkey"
        let button = height(of: Button("Choose Product") {}.buttonStyle(.dsSecondary(.small)), width: 288)
        let wide = height(of: row(note: nil, name: name), width: 560)
        // The iPhone 16e's row (the smallest the app supports) and a larger iPhone's.
        for width: CGFloat in [303, 322] {
            let narrow = height(of: row(note: nil, name: name), width: width)
            #expect(narrow - wide < button * 0.8, "at \(width): wide \(wide), narrow \(narrow), button \(button)")
        }
    }
}
