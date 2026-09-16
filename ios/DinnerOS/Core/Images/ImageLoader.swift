import Foundation
import UIKit
import os

/// What a scroll cost: how often a photo was already decoded, and how long the rest took.
nonisolated struct ImageLoaderStats: Sendable, Equatable {
    /// Requests answered from the decoded-image cache, with no work at all.
    var hits = 0
    /// Requests that had to fetch or decode.
    var misses = 0
    /// Misses that joined a fetch already in flight instead of starting a second one.
    var coalesced = 0
    /// Fetches that gave up, after their one retry.
    var failures = 0
    /// Fetches that produced an image, and the time they took together.
    var completed = 0
    var totalLoadNanoseconds: UInt64 = 0

    var requests: Int { hits + misses }

    /// Share of requests that needed no work; the number the Menu's smoothness follows.
    var hitRate: Double {
        requests > 0 ? Double(hits) / Double(requests) : 0
    }

    var averageLoadMilliseconds: Double {
        completed > 0 ? Double(totalLoadNanoseconds) / Double(completed) / 1_000_000 : 0
    }
}

/// Loads recipe photos once, at the size they're shown, and remembers them.
///
/// The Menu shows the same photo from several places and scrolls cards in and out constantly.
/// `AsyncImage` re-fetched and re-decoded every time, which is why cards spent so long blank.
/// This actor adds the three things it lacks: a decoded-image cache (`ImageMemoryCache`),
/// coalescing so several cards asking for one photo cause one fetch, and cancellation when the
/// last card waiting on a photo scrolls away.
actor ImageLoader {
    /// The app's loader.
    static let shared = ImageLoader()

    private let data: ImageDataLoading
    private let cache: ImageMemoryCache
    private let clock: @Sendable () -> UInt64
    private var entries: [ImageKey: Entry] = [:]
    private var stats = ImageLoaderStats()

    private static let signposter = OSSignposter(subsystem: "DinnerOS", category: "images")

    /// One fetch, and how many callers still want it. The fetch is cancelled when the count
    /// reaches zero, so scrolling past a card stops its download.
    private struct Entry {
        let task: Task<UIImage?, Never>
        var waiters: Int
    }

    init(
        data: ImageDataLoading = URLSessionImageDataLoader(),
        cache: ImageMemoryCache = .shared,
        clock: @escaping @Sendable () -> UInt64 = { DispatchTime.now().uptimeNanoseconds }
    ) {
        self.data = data
        self.cache = cache
        self.clock = clock
    }

    // MARK: - Loading

    /// The decoded image for `key`, or `nil` when it can't be loaded.
    ///
    /// Cancelling the calling task releases this caller's claim on the fetch; the fetch itself
    /// is only cancelled once nothing is waiting for it.
    func image(for key: ImageKey) async -> UIImage? {
        if let cached = cache.image(for: key) {
            stats.hits += 1
            return cached
        }
        stats.misses += 1
        let task = claim(key)
        return await withTaskCancellationHandler {
            await task.value
        } onCancel: {
            Task { await self.release(key) }
        }
    }

    /// The image for `key` if it's already decoded. Lets a view draw a cached photo on its
    /// first frame, with no placeholder in between.
    func cachedImage(for key: ImageKey) -> UIImage? {
        cache.image(for: key)
    }

    /// Starts loading `keys` without waiting, for cards that are about to scroll into view.
    /// Keys that are cached or already in flight cost nothing.
    func prefetch(_ keys: [ImageKey]) {
        for key in keys where cache.image(for: key) == nil {
            _ = claim(key)
        }
    }

    /// Drops a prefetch's claim on `keys`, cancelling any fetch nothing else is waiting for.
    func cancelPrefetch(_ keys: [ImageKey]) {
        for key in keys {
            release(key)
        }
    }

    // MARK: - Stats

    func statistics() -> ImageLoaderStats {
        stats
    }

    func resetStatistics() {
        stats = ImageLoaderStats()
    }

    /// Empties the decoded-image cache. Used by tests and by sign-out.
    func removeAll() {
        cache.removeAll()
    }

    // MARK: - Coalescing

    /// The fetch for `key`, started if it isn't running, with one more caller counted.
    private func claim(_ key: ImageKey) -> Task<UIImage?, Never> {
        if var entry = entries[key] {
            entry.waiters += 1
            entries[key] = entry
            stats.coalesced += 1
            return entry.task
        }
        let data = self.data
        let cache = self.cache
        let started = clock()
        let task = Task<UIImage?, Never> { [key] in
            let image = await ImageLoader.fetch(key: key, using: data)
            // Cached here, inside the fetch, so that a caller holding the returned photo knows
            // it is already in the cache. Storing it from the bookkeeping task instead left a
            // window where a card that scrolled away and back re-fetched a photo it had.
            if let image {
                cache.store(image, for: key)
            }
            return image
        }
        entries[key] = Entry(task: task, waiters: 1)
        // Finishing is its own task so the fetch can be cancelled without losing the bookkeeping.
        Task { await self.finish(key: key, task: task, started: started) }
        return task
    }

    private func release(_ key: ImageKey) {
        guard var entry = entries[key] else { return }
        entry.waiters -= 1
        if entry.waiters <= 0 {
            entry.task.cancel()
            entries[key] = nil
        } else {
            entries[key] = entry
        }
    }

    private func finish(key: ImageKey, task: Task<UIImage?, Never>, started: UInt64) async {
        let image = await task.value
        // Only clear the entry if it's still this fetch. A card that scrolled away and came
        // straight back has already started a new one under the same key, and dropping that
        // would let the next card start a second fetch for a photo already on its way.
        if entries[key]?.task == task {
            entries[key] = nil
        }
        if image != nil {
            stats.completed += 1
            stats.totalLoadNanoseconds += clock() &- started
        } else if !task.isCancelled {
            // A fetch nobody is waiting for any more was cancelled, not failed: scrolling past
            // a card is the expected case, and counting it would bury the real failures.
            stats.failures += 1
        }
    }

    // MARK: - Fetching

    /// Downloads and downsamples, retrying a transient failure once before giving up quietly.
    private static func fetch(key: ImageKey, using data: ImageDataLoading) async -> UIImage? {
        let state = signposter.beginInterval("load image")
        defer { signposter.endInterval("load image", state) }
        for attempt in 0...1 {
            if Task.isCancelled { return nil }
            do {
                let bytes = try await data.data(for: key.url)
                if Task.isCancelled { return nil }
                return ImageDownsampler.image(from: bytes, maxPixelSize: key.pixelSize)
            } catch is CancellationError {
                return nil
            } catch {
                guard attempt == 0, isTransient(error) else { return nil }
                // A short backoff: long enough to clear a blip, short enough that a card that
                // is still on screen gets its photo.
                do {
                    try await Task.sleep(for: .milliseconds(300))
                } catch {
                    return nil
                }
            }
        }
        return nil
    }

    /// A failure worth one more try: a dropped connection or a server that's briefly unhappy.
    /// A 404 photo is not retried, so a recipe whose image is gone settles on the glyph.
    private static func isTransient(_ error: Error) -> Bool {
        if let loadError = error as? ImageLoadError {
            switch loadError {
            case .invalidResponse: return false
            case .server(let status): return status == 429 || (500..<600).contains(status)
            }
        }
        guard let urlError = error as? URLError else { return false }
        switch urlError.code {
        case .timedOut, .networkConnectionLost, .cannotConnectToHost, .dnsLookupFailed,
            .notConnectedToInternet, .cannotFindHost:
            return true
        default:
            return false
        }
    }
}
