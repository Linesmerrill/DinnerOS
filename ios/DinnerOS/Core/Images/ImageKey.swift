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
    init?(url: URL?, pointWidth: CGFloat, scale: CGFloat) {
        guard let url else { return nil }
        let bucket = ImageKey.bucket(pointWidth: pointWidth, scale: scale)
        guard let sized = RecipeImageURL.sized(url, pixelWidth: bucket) as URL? else { return nil }
        self.init(url: sized, pixelSize: bucket)
    }

    /// The bucket covering `pointWidth` points on a `scale` display.
    ///
    /// A width that isn't known yet (zero, or a not-yet-measured layout) falls back to a
    /// mid-sized bucket instead of the 1200-pixel original, so a first frame never downloads a
    /// hero-sized photo for a card.
    static func bucket(pointWidth: CGFloat, scale: CGFloat) -> Int {
        guard pointWidth.isFinite, pointWidth > 0 else { return fallbackBucket }
        return RecipeImageURL.bucket(for: Int((pointWidth * max(scale, 1)).rounded(.up)))
    }

    /// Used until a layout-dependent width is measured. Wide enough for a full-width card on a
    /// phone, far short of the original.
    static let fallbackBucket = 640
}
