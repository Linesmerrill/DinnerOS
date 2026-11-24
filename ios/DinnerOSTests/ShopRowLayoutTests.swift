import SwiftUI
import Testing
import UIKit

@testable import DinnerOS

/// Where a Shop row's Choose Product button goes. On an iPad the Lemon row's note ("1 for this
/// meal · Also in Chicken Penne al Limone") pushed the button under the text, out of line with
/// every other row (Hark's review, build 123). The note wraps beside the button now; only a
/// row too narrow for a comfortable text column (an iPhone) puts the button underneath.
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

    private func row(note: String?) -> some View {
        NeedsProductRow(
            line: line("Lemon"), canChoose: true, mealNote: note,
            remove: ShopRemoveOptions(name: "Lemon", thisWeek: {}, always: {}), choose: {})
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

    @Test func aNarrowRowStillPutsTheButtonUnderTheText() {
        // An iPhone SE's row: the text would get a sliver of a column, so the button goes under it.
        let width: CGFloat = 288
        let wide = height(of: row(note: Self.longNote), width: 560)
        let narrow = height(of: row(note: Self.longNote), width: width)
        let button = height(of: Button("Choose Product") {}.buttonStyle(.dsSecondary(.small)), width: width)

        #expect(narrow - wide >= button * 0.8, "wide \(wide), narrow \(narrow), button \(button)")
    }
}
