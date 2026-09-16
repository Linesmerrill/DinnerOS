import CoreGraphics
import Foundation

/// What one decoded photo is cached under: the URL that was fetched and the pixel size it was
/// decoded for.
///
/// The size is a `RecipeImageURL` bucket rather than the exact measured pixels, for the same
/// reason the CDN request is bucketed: a card measured at 370.33 points and one at 371 would
/// otherwise be two cache entries holding two nearly identical bitmaps. Bucketing also keeps
/// the key aligned with the URL — the same bucket always means the same download.
nonisolated struct ImageKey: Hashable, Sendable {
    /// The URL actually fetched, already sized by `RecipeImageURL`.
    let url: URL
    /// The longest edge, in pixels, the image is decoded to.
    let pixelSize: Int

    init(url: URL, pixelSize: Int) {
        self.url = url
        self.pixelSize = max(pixelSize, 1)
    }

    /// The key for showing `url` at `pointWidth` points on a `scale` display.
    ///
    /// `nil` when there's no URL, which is how a recipe with no photo skips loading entirely.
    /// The URL is sized for the same bucket the image is decoded to, so the CDN sends roughly
    /// the pixels that are kept.
    /// `maxBucket` is the widest bucket this photo is allowed to ask for. It defaults to the
    /// card cap; the hero passes `nil` to keep the full-width original.
    init?(url: URL?, pointWidth: CGFloat, scale: CGFloat, maxBucket: Int? = ImageKey.cardBucketCap) {
        guard let url else { return nil }
        let bucket = ImageKey.bucket(pointWidth: pointWidth, scale: scale, maxBucket: maxBucket)
        guard let sized = RecipeImageURL.sized(url, pixelWidth: bucket) as URL? else { return nil }
        self.init(url: sized, pixelSize: bucket)
    }

    /// The bucket covering `pointWidth` points on a `scale` display, never wider than
    /// `maxBucket`.
    ///
    /// A width that isn't known yet (zero, or a not-yet-measured layout) falls back to a
    /// mid-sized bucket instead of the 1200-pixel original, so a first frame never downloads a
    /// hero-sized photo for a card.
    static func bucket(pointWidth: CGFloat, scale: CGFloat, maxBucket: Int? = nil) -> Int {
        guard pointWidth.isFinite, pointWidth > 0 else { return fallbackBucket }
        let measured = RecipeImageURL.bucket(for: Int((pointWidth * max(scale, 1)).rounded(.up)))
        guard let maxBucket else { return measured }
        return min(measured, RecipeImageURL.bucket(for: maxBucket))
    }

    /// The widest bucket a card asks for.
    ///
    /// A full-width card is about 370 points, which at @3x measures 1110 pixels and rounds up to
    /// the 1200-pixel bucket — the original, the same photo the full-screen hero gets. One bucket
    /// down is 1080 pixels: still more than the card can show on any phone, and about a fifth
    /// fewer bytes to download and decode. The hero is the only photo big enough to want 1200.
    static let cardBucketCap = 1080

    /// Used until a layout-dependent width is measured. Wide enough for a full-width card on a
    /// phone, far short of the original.
    static let fallbackBucket = 640
}
