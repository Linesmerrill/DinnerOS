import Foundation
import Testing
import UIKit

@testable import DinnerOS

/// A measurement harness, not part of the suite.
///
/// It needs a network and a list of real catalog URLs, so it only runs when
/// `DINNEROS_IMAGE_BENCHMARK=1` and `DINNEROS_IMAGE_URLS` names a file with one image URL per
/// line. Every ordinary run skips it, which is what keeps the suite off the network.
///
/// It replays a scroll through All Meals — forward, back over what was just seen, then forward
/// again — against two loaders: one standing in for `AsyncImage` (no memory cache, no
/// downsampling, no coalescing) and the real `ImageLoader`.
struct ImageLoaderBenchmarkTests {
    /// `nonisolated` because a `.enabled(if:)` trait is evaluated in a `@Sendable` closure,
    /// outside the main actor the test suite otherwise runs on.
    private nonisolated static var isEnabled: Bool {
        ProcessInfo.processInfo.environment["DINNEROS_IMAGE_BENCHMARK"] == "1"
    }

    /// Distinct cards touched, and the scroll pattern over them.
    private static let cardCount = 40
    private static let pointWidth: CGFloat = 400
    private static let scale: CGFloat = 3

    private static func catalogURLs() throws -> [URL] {
        let path = try #require(
            ProcessInfo.processInfo.environment["DINNEROS_IMAGE_URLS"],
            "Set DINNEROS_IMAGE_URLS to a file of image URLs")
        let text = try String(contentsOfFile: path, encoding: .utf8)
        return text.split(separator: "\n").compactMap { URL(string: String($0).trimmingCharacters(in: .whitespaces)) }
    }

    /// Forward through the cards, back over the last twenty, then forward again — the
    /// scroll-out-and-back that `AsyncImage` re-fetched every time.
    private static func scrollOrder(over count: Int) -> [Int] {
        let forward = Array(0..<count)
        let back = Array((count / 2..<count).reversed())
        return forward + back + Array(count / 2..<count)
    }

    @Test(.enabled(if: ImageLoaderBenchmarkTests.isEnabled))
    func measuresAScrollThroughAllMeals() async throws {
        let catalog = try Self.catalogURLs()
        let cards = Array(catalog.prefix(Self.cardCount))
        #expect(cards.count == Self.cardCount)
        let order = Self.scrollOrder(over: cards.count)

        let before = try await measureLegacy(cards: cards, order: order)
        let after = try await measureLoader(cards: cards, order: order)

        print("=== image loading: \(order.count) requests over \(cards.count) distinct photos ===")
        print(before.describe(label: "before (AsyncImage-equivalent)"))
        print(after.describe(label: "after  (ImageLoader)"))
    }

    // MARK: - Passes

    /// What `AsyncImage` did: a session with no cache, a full-size decode, one fetch per
    /// request, nothing remembered between them.
    private func measureLegacy(cards: [URL], order: [Int]) async throws -> Measurement {
        let configuration = URLSessionConfiguration.ephemeral
        configuration.urlCache = nil
        configuration.requestCachePolicy = .reloadIgnoringLocalCacheData
        let session = URLSession(configuration: configuration)
        // AsyncImage was given the same CDN-sized URL the card asks for today.
        let bucket = ImageKey.bucket(pointWidth: Self.pointWidth, scale: Self.scale)
        let sized = cards.map { RecipeImageURL.sized($0, pixelWidth: bucket) }

        var measurement = Measurement()
        var residentBytes = 0
        var seen = Set<URL>()
        let started = DispatchTime.now().uptimeNanoseconds
        for index in order {
            let url = sized[index]
            let requestStarted = DispatchTime.now().uptimeNanoseconds
            let (data, _) = try await session.data(from: url)
            guard let image = UIImage(data: data) else { continue }
            // Force the decode, the way drawing it would.
            _ = image.cgImage?.height
            measurement.completed += 1
            measurement.totalLoadNanoseconds += DispatchTime.now().uptimeNanoseconds &- requestStarted
            if seen.insert(url).inserted {
                residentBytes += ImageDownsampler.cost(of: image)
            }
        }
        measurement.requests = order.count
        // Nothing is cached, so every request is a miss.
        measurement.hits = 0
        measurement.wallNanoseconds = DispatchTime.now().uptimeNanoseconds &- started
        measurement.residentBytes = residentBytes
        return measurement
    }

    private func measureLoader(cards: [URL], order: [Int]) async throws -> Measurement {
        let cache = ImageMemoryCache()
        let loader = ImageLoader(data: URLSessionImageDataLoader(session: Self.freshSession()), cache: cache)
        let keys = cards.compactMap { ImageKey(url: $0, pointWidth: Self.pointWidth, scale: Self.scale) }
        #expect(keys.count == cards.count)

        var residentBytes = 0
        var seen = Set<ImageKey>()
        let started = DispatchTime.now().uptimeNanoseconds
        for index in order {
            let key = keys[index]
            guard let image = await loader.image(for: key) else { continue }
            if seen.insert(key).inserted {
                residentBytes += ImageDownsampler.cost(of: image)
            }
        }
        let wall = DispatchTime.now().uptimeNanoseconds &- started

        let stats = await loader.statistics()
        var measurement = Measurement()
        measurement.requests = stats.requests
        measurement.hits = stats.hits
        measurement.completed = stats.completed
        measurement.totalLoadNanoseconds = stats.totalLoadNanoseconds
        measurement.wallNanoseconds = wall
        measurement.residentBytes = residentBytes
        return measurement
    }

    /// A private session so a rerun measures fetching, not the last run's disk cache.
    private static func freshSession() -> URLSession {
        let configuration = URLSessionConfiguration.default
        configuration.urlCache = URLCache(memoryCapacity: 50 * 1024 * 1024, diskCapacity: 300 * 1024 * 1024)
        configuration.requestCachePolicy = .useProtocolCachePolicy
        configuration.httpMaximumConnectionsPerHost = 6
        return URLSession(configuration: configuration)
    }

    private struct Measurement {
        var requests = 0
        var hits = 0
        var completed = 0
        var totalLoadNanoseconds: UInt64 = 0
        var wallNanoseconds: UInt64 = 0
        var residentBytes = 0

        func describe(label: String) -> String {
            let hitRate = requests > 0 ? Double(hits) / Double(requests) * 100 : 0
            let average = completed > 0 ? Double(totalLoadNanoseconds) / Double(completed) / 1_000_000 : 0
            let wall = Double(wallNanoseconds) / 1_000_000_000
            let megabytes = Double(residentBytes) / 1_024 / 1_024
            return """
                \(label)
                  cache hit rate:   \(String(format: "%.1f", hitRate))% (\(hits)/\(requests))
                  loads performed:  \(completed)
                  average load:     \(String(format: "%.1f", average)) ms
                  whole scroll:     \(String(format: "%.2f", wall)) s
                  decoded at rest:  \(String(format: "%.1f", megabytes)) MB
                """
        }
    }
}
