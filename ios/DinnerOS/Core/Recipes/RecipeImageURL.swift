import CoreGraphics
import Foundation

/// Requests recipe photos at the size they're shown.
///
/// Imported image URLs carry a CDN transformation segment such as
/// `/f_auto,fl_lossy,q_auto,w_1200/`. Swapping its `w_<pixels>` token asks the CDN for a
/// narrower image, which downloads and decodes far less for a card than the 1200-pixel
/// original. Widths round up to a few buckets so the same photo at similar sizes shares one
/// cached response, and never exceed the original width. URLs without a width token are
/// returned unchanged, so other hosts keep working.
nonisolated enum RecipeImageURL {
    /// Pixel widths requested from the CDN.
    static let widthBuckets = [160, 320, 480, 640, 800, 1080, 1200]

    /// `url` sized for `pointWidth` points on a `scale` display; `nil` when `url` is `nil`.
    static func sized(_ url: URL?, pointWidth: CGFloat, scale: CGFloat) -> URL? {
        guard let url else { return nil }
        guard pointWidth.isFinite, pointWidth > 0 else { return url }
        let pixels = Int((pointWidth * max(scale, 1)).rounded(.up))
        return sized(url, pixelWidth: pixels)
    }

    /// `url` with its width token replaced by the bucket covering `pixelWidth`.
    static func sized(_ url: URL, pixelWidth: Int) -> URL {
        guard var components = URLComponents(url: url, resolvingAgainstBaseURL: false) else { return url }
        var segments = components.percentEncodedPath.split(separator: "/", omittingEmptySubsequences: false).map(
            String.init)
        for (segmentIndex, segment) in segments.enumerated() {
            var tokens = segment.split(separator: ",", omittingEmptySubsequences: false).map(String.init)
            guard let tokenIndex = tokens.firstIndex(where: { width(in: $0) != nil }),
                let original = width(in: tokens[tokenIndex])
            else { continue }
            let requested = min(bucket(for: pixelWidth), original)
            guard requested != original else { return url }
            tokens[tokenIndex] = "w_\(requested)"
            segments[segmentIndex] = tokens.joined(separator: ",")
            components.percentEncodedPath = segments.joined(separator: "/")
            return components.url ?? url
        }
        return url
    }

    /// The smallest bucket at least `pixels` wide, or the largest bucket.
    static func bucket(for pixels: Int) -> Int {
        widthBuckets.first { $0 >= pixels } ?? widthBuckets.last ?? pixels
    }

    /// The pixels in a `w_<digits>` token; `nil` for any other token.
    private static func width(in token: String) -> Int? {
        guard token.hasPrefix("w_") else { return nil }
        let digits = token.dropFirst(2)
        guard !digits.isEmpty, digits.allSatisfy(\.isASCII), digits.allSatisfy(\.isNumber) else { return nil }
        return Int(digits)
    }
}
