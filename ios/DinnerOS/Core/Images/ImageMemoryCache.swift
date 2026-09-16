import Foundation
import UIKit

/// Decoded photos, held in memory until they're evicted.
///
/// `AsyncImage` has no memory cache at all, so a card that scrolled away and came back
/// re-downloaded and re-decoded its photo. This keeps the decoded bitmaps, bounded by a cost
/// limit in bytes and emptied on a memory warning.
nonisolated final class ImageMemoryCache: Sendable {
    /// The app's cache. Tests make their own.
    static let shared = ImageMemoryCache()

    /// About sixteen full-screen photos, or several hundred card-sized ones.
    static let defaultCostLimit = 64 * 1024 * 1024

    /// `NSCache` is thread-safe but isn't marked `Sendable`, so the guarantee is asserted here
    /// rather than wrapping every read in a lock that `NSCache` already holds.
    private nonisolated(unsafe) let cache = NSCache<Key, UIImage>()

    /// `notificationCenter` is injectable so a test can post a memory warning to its own centre:
    /// on the default one it would empty every other cache in the process at the same time.
    init(
        costLimit: Int = ImageMemoryCache.defaultCostLimit, countLimit: Int = 0,
        notificationCenter: NotificationCenter = .default
    ) {
        cache.totalCostLimit = costLimit
        cache.countLimit = countLimit
        // NSCache already drops objects under pressure, but not promptly enough to keep a long
        // scroll from being the reason the app is killed.
        // Capturing the cache itself would warn: `NSCache` isn't `Sendable`. The cache is, so
        // the closure goes through it.
        notificationCenter.addObserver(
            forName: UIApplication.didReceiveMemoryWarningNotification, object: nil, queue: nil
        ) { [self] _ in
            removeAll()
        }
    }

    func image(for key: ImageKey) -> UIImage? {
        cache.object(forKey: Key(key))
    }

    func store(_ image: UIImage, for key: ImageKey) {
        cache.setObject(image, forKey: Key(key), cost: ImageDownsampler.cost(of: image))
    }

    func removeAll() {
        cache.removeAllObjects()
    }

    /// `NSCache` keys have to be objects, so `ImageKey` is boxed.
    private final class Key: NSObject {
        let value: ImageKey

        init(_ value: ImageKey) {
            self.value = value
        }

        override var hash: Int {
            value.hashValue
        }

        override func isEqual(_ object: Any?) -> Bool {
            guard let other = object as? Key else { return false }
            return other.value == value
        }
    }
}
