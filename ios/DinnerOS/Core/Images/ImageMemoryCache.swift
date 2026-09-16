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

    /// How many bytes of decoded photos to keep, for this device.
    static var defaultCostLimit: Int {
        costLimit(physicalMemory: ProcessInfo.processInfo.physicalMemory)
    }

    /// A sixteenth of the device's memory, between `minimumCostLimit` and `maximumCostLimit`.
    ///
    /// A flat 64 MB was under the working set: a long All Meals scroll touches about 119 MB of
    /// decoded photos, so `NSCache` evicted photos that were about to be scrolled back to and
    /// the hit rate sat below what the scroll pattern allows. Sizing off the device instead of a
    /// constant keeps the cache proportionate — a phone with 8 GB can hold a whole scroll, one
    /// with 2 GB holds less and evicts sooner, which is the right answer on both. The floor
    /// keeps a small device from thrashing on every card; the ceiling keeps the cache from
    /// becoming the reason a big one is killed. Either way it stays bounded, `NSCache` still
    /// evicts, and a memory warning still empties it outright.
    static func costLimit(physicalMemory: UInt64) -> Int {
        let share = physicalMemory / 16
        return Int(min(max(share, UInt64(minimumCostLimit)), UInt64(maximumCostLimit)))
    }

    /// Enough for a screen of cards and the few on either side of it.
    static let minimumCostLimit = 48 * 1024 * 1024
    /// Comfortably over the 119 MB a long scroll touches, without holding much more than that.
    static let maximumCostLimit = 192 * 1024 * 1024

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
