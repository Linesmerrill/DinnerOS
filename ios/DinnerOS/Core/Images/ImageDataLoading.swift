import Foundation

/// Fetches the bytes of one image. `ImageLoader` depends on this instead of `URLSession` so
/// tests can answer without a network.
nonisolated protocol ImageDataLoading: Sendable {
    func data(for url: URL) async throws -> Data
}

nonisolated enum ImageLoadError: Error, Equatable {
    case invalidResponse
    case server(status: Int)
}

/// The production loader: one shared session with a large on-disk cache.
nonisolated struct URLSessionImageDataLoader: ImageDataLoading {
    let session: URLSession

    init(session: URLSession = URLSessionImageDataLoader.makeSession()) {
        self.session = session
    }

    func data(for url: URL) async throws -> Data {
        let request = URLRequest(url: url, cachePolicy: .useProtocolCachePolicy, timeoutInterval: 20)
        let (bytes, response) = try await session.data(for: request)
        guard let http = response as? HTTPURLResponse else { throw ImageLoadError.invalidResponse }
        guard (200..<300).contains(http.statusCode) else { throw ImageLoadError.server(status: http.statusCode) }
        return bytes
    }

    /// Photos are immutable and served with caching headers, so unlike the API session
    /// (`URLSessionTransport`, which caches nothing) this one keeps a generous `URLCache`.
    /// The disk half is what makes a relaunch cheap: the bytes are already there, and only the
    /// decode is repeated.
    static func makeSession() -> URLSession {
        let configuration = URLSessionConfiguration.default
        configuration.urlCache = URLCache(
            memoryCapacity: 50 * 1024 * 1024, diskCapacity: 300 * 1024 * 1024, directory: nil)
        configuration.requestCachePolicy = .useProtocolCachePolicy
        configuration.httpMaximumConnectionsPerHost = 6
        configuration.timeoutIntervalForRequest = 20
        configuration.waitsForConnectivity = false
        return URLSession(configuration: configuration)
    }
}
