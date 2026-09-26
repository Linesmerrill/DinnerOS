import Foundation

/// One call to the DinnerOS API.
nonisolated struct APIRequest: Sendable {
    enum Method: String, Sendable {
        case get = "GET"
        case post = "POST"
        case patch = "PATCH"
        case put = "PUT"
        case delete = "DELETE"
    }

    var method: Method
    /// Absolute path under the base URL, for example `/api/v1/me`.
    var path: String
    var body: Data?
    var bearerToken: String?
    /// Sent in order. Names and values are percent-encoded strictly, so `+` and `&` in a
    /// value arrive as written.
    var queryItems: [URLQueryItem] = []
    /// `path` is already percent-encoded (see `encodePathSegment`) and is sent as is. Otherwise
    /// the path is encoded when the URL is built, which would encode a `%` a second time.
    var isPathPercentEncoded = false

    /// A path segment with every character outside RFC 3986's unreserved set and `:`
    /// percent-encoded, so a `/`, `%`, or space inside an ID arrives as one segment.
    static func encodePathSegment(_ segment: String) -> String {
        segment.addingPercentEncoding(withAllowedCharacters: pathSegmentAllowed) ?? ""
    }

    private static let pathSegmentAllowed = CharacterSet(
        charactersIn: "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789-._~:")

    /// Marks `path` as already percent-encoded.
    func withPercentEncodedPath() -> APIRequest {
        var copy = self
        copy.isPathPercentEncoded = true
        return copy
    }

    static func get(_ path: String) -> APIRequest {
        APIRequest(method: .get, path: path, body: nil, bearerToken: nil)
    }

    static func post(_ path: String, body: some Encodable) throws -> APIRequest {
        APIRequest(method: .post, path: path, body: try JSONCoding.makeEncoder().encode(body), bearerToken: nil)
    }

    static func patch(_ path: String, body: some Encodable) throws -> APIRequest {
        APIRequest(method: .patch, path: path, body: try JSONCoding.makeEncoder().encode(body), bearerToken: nil)
    }

    static func put(_ path: String, body: some Encodable) throws -> APIRequest {
        APIRequest(method: .put, path: path, body: try JSONCoding.makeEncoder().encode(body), bearerToken: nil)
    }

    static func delete(_ path: String) -> APIRequest {
        APIRequest(method: .delete, path: path, body: nil, bearerToken: nil)
    }

    func authorized(with accessToken: String) -> APIRequest {
        var copy = self
        copy.bearerToken = accessToken
        return copy
    }
}

/// Thin async/await client for the DinnerOS API.
///
/// Encoding, the network round trip, and decoding run off the main actor. The client
/// knows nothing about sessions: `AuthSession` attaches tokens and handles refresh.
/// Nothing here logs request bodies or headers, because they carry tokens.
nonisolated struct APIClient: Sendable {
    let baseURL: URL
    let transport: any HTTPTransport
    let retry: RetryPolicy

    /// `retry` defaults to `.standard` for the real network and `.none` for a substituted
    /// transport, so a test stub answers once unless the test asks for retries.
    init(baseURL: URL, transport: any HTTPTransport = URLSessionTransport(), retry: RetryPolicy? = nil) {
        self.baseURL = baseURL
        self.transport = transport
        self.retry = retry ?? (transport is URLSessionTransport ? .standard : .none)
    }

    /// Sends `request` and decodes a JSON response body.
    @concurrent
    func send<Response: Decodable & Sendable>(
        _ request: APIRequest, as type: Response.Type = Response.self
    ) async throws -> Response {
        let data = try await perform(request)
        do {
            return try JSONCoding.makeDecoder().decode(Response.self, from: data)
        } catch {
            throw APIError.decoding(type: String(describing: Response.self))
        }
    }

    /// Sends `request` and ignores any response body (for example, `204 No Content`).
    @concurrent
    func sendIgnoringBody(_ request: APIRequest) async throws {
        _ = try await perform(request)
    }

    func makeURLRequest(for request: APIRequest, requestID: String) -> URLRequest {
        let relativePath = request.path.hasPrefix("/") ? String(request.path.dropFirst()) : request.path
        var url = baseURL.appending(path: relativePath)
        if request.isPathPercentEncoded, var components = URLComponents(url: baseURL, resolvingAgainstBaseURL: false) {
            let basePath = components.percentEncodedPath
            components.percentEncodedPath = (basePath.hasSuffix("/") ? basePath : basePath + "/") + relativePath
            url = components.url ?? url
        }
        if !request.queryItems.isEmpty, var components = URLComponents(url: url, resolvingAgainstBaseURL: false) {
            components.percentEncodedQuery = request.queryItems
                .map { Self.encodeQueryComponent($0.name) + "=" + Self.encodeQueryComponent($0.value ?? "") }
                .joined(separator: "&")
            url = components.url ?? url
        }
        var urlRequest = URLRequest(url: url)
        urlRequest.httpMethod = request.method.rawValue
        urlRequest.setValue("application/json", forHTTPHeaderField: "Accept")
        urlRequest.setValue(requestID, forHTTPHeaderField: "X-Request-ID")
        if let body = request.body {
            urlRequest.httpBody = body
            urlRequest.setValue("application/json", forHTTPHeaderField: "Content-Type")
        }
        if let token = request.bearerToken {
            urlRequest.setValue("Bearer \(token)", forHTTPHeaderField: "Authorization")
        }
        return urlRequest
    }

    /// RFC 3986 unreserved characters only. `URLQueryItem`'s default encoding leaves `+`
    /// as is, which Go's query parser reads as a space.
    private static let queryAllowed = CharacterSet(
        charactersIn: "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789-._~")

    static func encodeQueryComponent(_ string: String) -> String {
        string.addingPercentEncoding(withAllowedCharacters: queryAllowed) ?? ""
    }

    /// Sends `request`, retrying the failures that mean the request never reached the API
    /// (`RetryPolicy`). Every attempt reuses one request ID, so the server's logs tie them together.
    private func perform(_ request: APIRequest) async throws -> Data {
        let requestID = UUID().uuidString
        var delays = retry.delays[...]
        while true {
            do {
                return try await performOnce(request, requestID: requestID)
            } catch let error as APIError {
                guard let delay = delays.popFirst(), RetryPolicy.isRetryable(error, method: request.method) else {
                    throw error
                }
                try await retry.sleep(delay)
            }
        }
    }

    private func performOnce(_ request: APIRequest, requestID: String) async throws -> Data {
        let urlRequest = makeURLRequest(for: request, requestID: requestID)

        let data: Data
        let response: HTTPURLResponse
        do {
            (data, response) = try await transport.send(urlRequest)
        } catch let error as APIError {
            throw error
        } catch let error as URLError {
            if error.code == .cancelled { throw CancellationError() }
            throw APIError.transport(error.code)
        } catch is CancellationError {
            throw CancellationError()
        } catch {
            throw APIError.transport(.unknown)
        }

        guard (200..<300).contains(response.statusCode) else {
            throw APIError(
                status: response.statusCode,
                body: data,
                fallbackRequestID: response.value(forHTTPHeaderField: "X-Request-ID") ?? requestID)
        }
        return data
    }
}

/// Which failed requests are worth sending again, and how long to wait between tries.
///
/// The API runs on a Heroku Eco dyno that sleeps after 30 idle minutes. The first requests
/// after that can fail while it wakes: the router answers `503` (H99, H10) or the connection
/// can't be made. Those requests never reached the API, so sending them again is safe and is
/// what a person would do — and a sleeping server shouldn't show up as a save error.
///
/// Only failures that *didn't reach the API* are retried. A `POST` is retried only when no
/// connection was made at all, since any later failure might have created something; the other
/// methods are idempotent, so a gateway error or a dropped connection is retried too. Being
/// offline is not retried: waiting wouldn't change it, and the member should hear at once.
nonisolated struct RetryPolicy: Sendable {
    /// The wait before each retry. Three tries after the first cover a dyno waking (about
    /// 5–10 seconds, measured from the router's connect times) without hanging much longer.
    let delays: [Duration]
    let sleep: @Sendable (Duration) async throws -> Void

    init(
        delays: [Duration],
        sleep: @escaping @Sendable (Duration) async throws -> Void = { try await Task.sleep(for: $0) }
    ) {
        self.delays = delays
        self.sleep = sleep
    }

    static let standard = RetryPolicy(delays: [.milliseconds(1500), .seconds(3), .seconds(6)])
    static let none = RetryPolicy(delays: [])

    /// Connection failures where nothing was sent.
    private static let neverConnected: Set<URLError.Code> = [.cannotConnectToHost, .cannotFindHost, .dnsLookupFailed]
    /// Failures after a connection, where an idempotent request can safely go again.
    private static let droppedConnection: Set<URLError.Code> = [.networkConnectionLost, .timedOut]
    /// The router's own "the app isn't answering" statuses.
    private static let gatewayStatuses: Set<Int> = [502, 503, 504]

    static func isRetryable(_ error: APIError, method: APIRequest.Method) -> Bool {
        let idempotent = method != .post
        switch error {
        case .transport(let code):
            return neverConnected.contains(code) || (idempotent && droppedConnection.contains(code))
        case .server(let status, _, _, _):
            return idempotent && gatewayStatuses.contains(status)
        case .invalidResponse, .decoding:
            return false
        }
    }
}
