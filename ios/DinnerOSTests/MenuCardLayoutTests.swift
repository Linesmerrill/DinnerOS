import SwiftUI
import Testing
import UIKit

@testable import DinnerOS

/// What a card's name is allowed to do: wrap to the two lines the card reserves for it, while
/// every card in a row stays the same height.
///
/// Measured by rendering the real view and looking at the pixels, because the bug this covers —
/// a name truncated to one line inside a two-line box — is a layout result, not a value any
/// property exposes.
@MainActor
struct MenuCardLayoutTests {
    private static let longName = "One-Pan Santa Fe Pork Tacos with Charred Corn"
    private static let shortName = "Skillet Tacos"
    private static let cardWidth: CGFloat = 220

    // MARK: - Rendering

    /// `view` drawn at `size`, through a real window so SwiftUI lays it out the way the app does.
    private func render(_ view: some View, size: CGSize) -> UIImage {
        let controller = UIHostingController(rootView: AnyView(view))
        let window = UIWindow(frame: CGRect(origin: .zero, size: size))
        window.rootViewController = controller
        window.isHidden = false
        controller.view.frame = CGRect(origin: .zero, size: size)
        controller.view.backgroundColor = .white
        window.layoutIfNeeded()
        controller.view.setNeedsLayout()
        controller.view.layoutIfNeeded()
        let renderer = UIGraphicsImageRenderer(size: size)
        return renderer.image { context in
            UIColor.white.setFill()
            context.fill(CGRect(origin: .zero, size: size))
            window.layer.render(in: context.cgContext)
        }
    }

    /// The rows of `image`, in points, that contain something darker than the background.
    private func inkRows(in image: UIImage) -> Set<Int> {
        guard let cgImage = image.cgImage else { return [] }
        let width = cgImage.width
        let height = cgImage.height
        var pixels = [UInt8](repeating: 255, count: width * height)
        guard
            let context = pixels.withUnsafeMutableBytes({ buffer in
                CGContext(
                    data: buffer.baseAddress, width: width, height: height, bitsPerComponent: 8, bytesPerRow: width,
                    space: CGColorSpaceCreateDeviceGray(), bitmapInfo: CGImageAlphaInfo.none.rawValue)
            })
        else { return [] }
        context.draw(cgImage, in: CGRect(x: 0, y: 0, width: width, height: height))
        let scale = Int(image.scale.rounded())
        var rows: Set<Int> = []
        for y in 0..<height where (0..<width).contains(where: { pixels[y * width + $0] < 140 }) {
            rows.insert(y / max(scale, 1))
        }
        return rows
    }

    /// How many separate horizontal bands of ink `rows` forms — one per rendered line of text.
    private func bandCount(in rows: Set<Int>) -> Int {
        let sorted = rows.sorted()
        guard let first = sorted.first else { return 0 }
        var bands = 1
        var previous = first
        for row in sorted.dropFirst() {
            // A gap wider than a couple of points is the space between two lines.
            if row - previous > 2 { bands += 1 }
            previous = row
        }
        return bands
    }

    /// A card's name, in the container a carousel card puts it in: a fixed-width column inside a
    /// horizontal scroll view, which proposes no width of its own.
    private func titleInACard(name: String) -> some View {
        ScrollView(.horizontal) {
            LazyHStack(alignment: .top, spacing: 12) {
                VStack(alignment: .leading, spacing: 8) {
                    CardTitle(name: name)
                }
                .frame(width: Self.cardWidth)
            }
        }
    }

    // MARK: - Tests

    @Test func aLongNameWrapsToTwoLines() {
        let size = CGSize(width: 393, height: 120)
        let rows = inkRows(in: render(titleInACard(name: Self.longName), size: size))
        #expect(bandCount(in: rows) == 2, "a long name should use both reserved lines, not truncate to one")
    }

    @Test func aShortNameStaysOnOneLine() {
        let size = CGSize(width: 393, height: 120)
        let rows = inkRows(in: render(titleInACard(name: Self.shortName), size: size))
        #expect(bandCount(in: rows) == 1)
    }

    @Test func aNameReservesTheSameHeightWhetherItWrapsOrNot() {
        let long = UIHostingController(rootView: CardTitle(name: Self.longName))
        let short = UIHostingController(rootView: CardTitle(name: Self.shortName))
        let proposal = CGSize(width: Self.cardWidth, height: UIView.layoutFittingCompressedSize.height)
        #expect(long.sizeThatFits(in: proposal).height == short.sizeThatFits(in: proposal).height)
    }
}
