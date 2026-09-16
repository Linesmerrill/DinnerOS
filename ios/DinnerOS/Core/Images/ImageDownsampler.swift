import Foundation
import ImageIO
import UIKit

/// Decodes image bytes straight to the size they're shown at.
///
/// `UIImage(data:)` keeps the full-resolution bitmap alive: a 1200×900 photo costs about 4 MB
/// of memory whether it fills the screen or a 160-point card. `CGImageSourceCreateThumbnailAtIndex`
/// decodes once, at the target size, so the same card costs about 300 KB.
nonisolated enum ImageDownsampler {
    /// `data` decoded so its longest edge is at most `maxPixelSize`; `nil` when it isn't an image.
    ///
    /// Call this off the main actor: decoding is CPU work, and the point is to keep it off the
    /// frame that's scrolling.
    static func image(from data: Data, maxPixelSize: Int) -> UIImage? {
        let sourceOptions = [kCGImageSourceShouldCache: false] as CFDictionary
        guard let source = CGImageSourceCreateWithData(data as CFData, sourceOptions) else { return nil }
        let thumbnailOptions =
            [
                kCGImageSourceCreateThumbnailFromImageAlways: true,
                // Honors EXIF orientation, so a portrait photo isn't decoded sideways.
                kCGImageSourceCreateThumbnailWithTransform: true,
                // Decodes now, on this thread, instead of lazily during the first draw.
                kCGImageSourceShouldCacheImmediately: true,
                kCGImageSourceThumbnailMaxPixelSize: max(maxPixelSize, 1),
            ] as [CFString: Any] as CFDictionary
        guard let thumbnail = CGImageSourceCreateThumbnailAtIndex(source, 0, thumbnailOptions) else { return nil }
        return UIImage(cgImage: thumbnail)
    }

    /// Roughly what `image` occupies in memory, for the cache's cost accounting.
    static func cost(of image: UIImage) -> Int {
        guard let cgImage = image.cgImage else { return 0 }
        return cgImage.bytesPerRow * cgImage.height
    }
}
