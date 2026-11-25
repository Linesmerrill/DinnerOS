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

    private func line(_ name: String, amount: String = "2") -> ShoppingExcludedLine {
        ShoppingExcludedLine(
            ingredientKey: "name:\(name.lowercased())", ingredientID: nil, name: name, category: "produce",
            amounts: [], quantityText: amount, unquantified: false, groceryStatus: .toBuy, reason: .noProduct,
            text: "Choose a product", searchTerms: ShoppingSearchTerms.plain(name))
    }

    private func height(of view: some View, width: CGFloat) -> CGFloat {
        let controller = UIHostingController(
            rootView: AnyView(view.environment(\.dynamicTypeSize, .large).frame(width: width)))
        return controller.sizeThatFits(in: CGSize(width: width, height: .greatestFiniteMagnitude)).height
    }

    private func row(note: String?, name: String = "Lemon", amount: String = "2") -> some View {
        NeedsProductRow(
            line: line(name, amount: amount), canChoose: true, mealNote: note,
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

    // MARK: - Two lines at most beside the button

    /// An iPhone SE (375 points wide) gives a Shop row under a meal 291 points: the
    /// shop-scrolled-to-flour shot's row runs from x 48 to 339.
    private static let seRowWidth: CGFloat = 291

    /// The name and amount on their own, one line each at a row's full width.
    private func textHeight(name: String, amount: String, width: CGFloat) -> CGFloat {
        height(
            of: VStack(alignment: .leading, spacing: DS.Space.hairline) {
                Text(name)
                Text(amount).font(.subheadline)
            }.frame(maxWidth: .infinity, alignment: .leading),
            width: width)
    }

    private var buttonHeight: CGFloat {
        height(of: Button("Choose Product") {}.buttonStyle(.dsSecondary(.small)), width: Self.seRowWidth)
    }

    @Test func onAnSEFlourGetsTheRowsWidthAndTheButtonGoesUnder() {
        // "All-Purpose Flour / 2 Tbsp + 2 tsp" took 5 lines beside Choose Product on an iPhone SE
        // (Hark's review, build 121). The name would need 3 lines beside it, so the button goes
        // on a line of its own and the name and amount read on one line each.
        for amount in ["2 Tbsp + 2 tsp", "2 ½ Tbsp"] {
            // On an iPad's width the row is one line high: as tall as its buttons, or its text.
            let buttonsLine = height(of: row(note: nil, name: "All-Purpose Flour", amount: amount), width: 560)
            for width: CGFloat in [288, Self.seRowWidth] {
                let text = textHeight(name: "All-Purpose Flour", amount: amount, width: width)
                let row = height(of: row(note: nil, name: "All-Purpose Flour", amount: amount), width: width)
                #expect(
                    row >= text + buttonHeight * 0.9,
                    "\(amount) at \(width): row \(row), text \(text), button \(buttonHeight); want the button under")
                // The text a line each, then the buttons: no wrapped line (about 20 points) on
                // top, only a few points of spacing.
                #expect(
                    row <= text + DS.Space.s + buttonsLine + 4,
                    "\(amount) at \(width): row \(row), text \(text), buttons \(buttonsLine); want one line each")
            }
        }
    }

    @Test func onAnSELemonKeepsTheButtonBesideIt() {
        // Last round's fix: the Lemon row's Choose Product stays on the row (small, large, Duo).
        let wide = height(of: row(note: nil, amount: "1"), width: 560)
        let wideNoted = height(of: row(note: Self.longNote, amount: "1"), width: 560)
        for width: CGFloat in [288, Self.seRowWidth, 303, 322] {
            let narrow = height(of: row(note: nil, amount: "1"), width: width)
            #expect(narrow - wide < buttonHeight * 0.5, "at \(width): wide \(wide), narrow \(narrow)")
            let noted = height(of: row(note: Self.longNote, amount: "1"), width: width)
            #expect(noted - wideNoted < buttonHeight * 0.8, "at \(width): wide \(wideNoted), narrow \(noted)")
        }
    }

    @Test func aNameThatFitsInTwoLinesStaysBesideTheButton() {
        // Two lines beside the button is fine: "Ground Turkey" wraps beside it at 303 points.
        let wide = height(of: row(note: nil, name: "Ground Turkey"), width: 560)
        let narrow = height(of: row(note: nil, name: "Ground Turkey"), width: 303)
        #expect(narrow - wide < buttonHeight * 0.8, "wide \(wide), narrow \(narrow)")
    }
}
