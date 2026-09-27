import CoreGraphics
import Foundation

/// A piece of text recognition found on a screenshot, with where it sits.
///
/// Coordinates are normalized to the image (0…1) with the origin at the **top left**, so a larger
/// `minY` is further down the screen. Vision's own boxes start at the bottom left;
/// `OrderScreenshotReader` flips them once, on the way in, so everything after that reads top to
/// bottom like the screen does.
nonisolated struct RecognizedPiece: Hashable, Sendable {
    var text: String
    var rect: CGRect

    init(_ text: String, _ rect: CGRect) {
        self.text = text
        self.rect = rect
    }
}

/// One visual row of a screenshot: everything printed on that line, joined left to right.
nonisolated struct RecognizedRow: Hashable, Sendable {
    var text: String
    /// The row's box, or `.null` when the text arrived without a layout (plain text, older tests).
    var rect: CGRect

    init(text: String, rect: CGRect = .null) {
        self.text = text
        self.rect = rect
    }

    /// Whether this row knows where it sits, so it can be read by geometry and not just order.
    var hasLayout: Bool { !rect.isNull }

    /// The same row moved down by whole screenshots. Every image's boxes are normalized to
    /// itself, so without this the rows of the second screenshot would sit among the first's.
    func movedDown(byScreens screens: Int) -> RecognizedRow {
        guard hasLayout, screens != 0 else { return self }
        return RecognizedRow(text: text, rect: rect.offsetBy(dx: 0, dy: CGFloat(screens)))
    }
}

/// Where an item was read from: which screenshot, and the box its title and price sit in.
///
/// The review screen crops the member's own screenshot to this box so a title the Walmart app cut
/// off with "…" can still be told apart by its photo. Only the box is kept here; the image stays
/// with the review screen, in memory, and is never saved.
nonisolated struct OrderItemSource: Hashable, Sendable {
    /// The screenshot's position in the member's selection.
    let screen: Int
    /// Normalized to that screenshot (0…1) with the origin at the top left, like `RecognizedRow`.
    let rect: CGRect

    /// The source of rows spanning `rect` in stacked-screenshot coordinates, kept to the
    /// screenshot the title (`anchor`) is on: a card cut by the bottom of one screenshot and
    /// repeated at the top of the next is shown from where its title was read.
    static func stacked(_ rect: CGRect, anchor: CGRect) -> OrderItemSource? {
        guard !rect.isNull, !anchor.isNull else { return nil }
        let screen = Int(anchor.midY.rounded(.down))
        let band = CGRect(x: -1, y: CGFloat(screen), width: 3, height: 1)
        let clipped = rect.intersection(band)
        guard !clipped.isNull, !clipped.isEmpty else { return nil }
        return OrderItemSource(screen: screen, rect: clipped.offsetBy(dx: 0, dy: -CGFloat(screen)))
    }

    /// The part of a screenshot `size` pixels big to show for this item, in pixels with the
    /// origin at the top left.
    ///
    /// Recognition only boxes text, so the photo's box is inferred: it sits left of the text
    /// column, about as wide as the space there, beside the title. The crop spans the whole width
    /// (photo, title, and a price at the right), pads the rows above and below, and is at least as
    /// tall as that photo would be.
    func cropRect(in size: CGSize) -> CGRect {
        guard size.width > 0, size.height > 0 else { return .null }
        let top = rect.minY * size.height
        let bottom = rect.maxY * size.height
        let padding = max(size.height * 0.012, 8)
        // The photo fills most of the space left of the column and is roughly square.
        let photo = rect.minX * size.width * 1.1
        let height = min(max(bottom - top + padding * 2, photo + padding * 2), size.height)
        var minY = (top + bottom) / 2 - height / 2
        minY = min(max(minY, 0), size.height - height)
        return CGRect(x: 0, y: minY, width: size.width, height: height).integral
            .intersection(CGRect(origin: .zero, size: size))
    }
}

/// Turns recognized pieces into the rows a person sees.
///
/// Two things need the layout rather than the reading order. Rows are grouped by their vertical
/// centres, because recognition returns a price and the name beside it as separate pieces. And
/// the Walmart app prints a price as large dollars with small, raised cents ("$1" with "26" above
/// the baseline), which arrive as two pieces: joined with a space they read as "$1 26" and parse
/// as $126.00, so they're rejoined here with the decimal point the screen only implies.
nonisolated enum OrderTextLayout {
    static func rows(_ pieces: [RecognizedPiece]) -> [RecognizedRow] {
        let sorted = withoutProductPhotos(pieces).sorted { $0.rect.midY < $1.rect.midY }
        var grouped: [[RecognizedPiece]] = []
        for piece in sorted {
            if let last = grouped.last?.last,
                abs(last.rect.midY - piece.rect.midY) < max(last.rect.height, piece.rect.height) / 2
            {
                grouped[grouped.count - 1].append(piece)
            } else {
                grouped.append([piece])
            }
        }
        return grouped.map { row in
            let ordered = row.sorted { $0.rect.minX < $1.rect.minX }
            let rect = ordered.dropFirst().reduce(ordered[0].rect) { $0.union($1.rect) }
            return RecognizedRow(text: join(ordered), rect: rect)
        }
    }

    /// Drops the words recognition picks out of the product photos.
    ///
    /// A card's photo sits to the left of the column its price and name are in, and the packaging
    /// in it is printed text too, so "Sour Cream Original" off the tub lands on the same row as
    /// the name and reads as part of it. The column is where most of the screen's text begins;
    /// anything that ends before it belongs to a photo. A screen whose text doesn't line up in a
    /// column (or has no room for photos) is left alone.
    static func withoutProductPhotos(_ pieces: [RecognizedPiece]) -> [RecognizedPiece] {
        let bucketWidth = 0.02
        let counted = Dictionary(grouping: pieces) { ($0.rect.minX / bucketWidth).rounded(.down) }
        let column =
            counted
            .filter { $0.value.count >= 4 }
            .min { left, right in
                left.value.count != right.value.count ? left.value.count > right.value.count : left.key < right.key
            }
        guard let start = column?.value.map(\.rect.minX).min(), start >= 0.1 else { return pieces }
        // A photo's words end before the column's text begins, however close they come to it.
        return pieces.filter { $0.rect.maxX > start }
    }

    /// The row's text, with raised cents joined onto their dollars as "$1.26".
    static func join(_ pieces: [RecognizedPiece]) -> String {
        var text = ""
        for (index, piece) in pieces.enumerated() {
            if index == 0 {
                text = piece.text
            } else if isRaisedCents(after: pieces[index - 1], piece) {
                text += "." + piece.text
            } else {
                text += " " + piece.text
            }
        }
        return text
    }

    /// Whether `cents` is the small, raised cents of the dollar amount `dollars` ends with:
    /// two digits, in smaller type, sitting higher, and right next to it.
    static func isRaisedCents(after dollars: RecognizedPiece, _ cents: RecognizedPiece) -> Bool {
        guard cents.text.wholeMatch(of: #/\d{2}/#) != nil else { return false }
        guard dollars.text.firstMatch(of: #/\$\s?\d{1,4}$/#) != nil else { return false }
        guard cents.rect.height < dollars.rect.height * 0.85 else { return false }
        guard cents.rect.midY < dollars.rect.midY else { return false }
        let gap = cents.rect.minX - dollars.rect.maxX
        return gap >= -dollars.rect.height && gap <= dollars.rect.height
    }
}
