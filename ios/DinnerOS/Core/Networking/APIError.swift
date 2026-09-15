import Foundation

/// Every failure `APIClient` reports.
nonisolated enum APIError: Error, Equatable, Sendable {
    /// A non-2xx response. `code` comes from the backend envelope
    /// `{"error": {"code", "message", "requestId"}}`, or is `http_<status>` when the
    /// body isn't an envelope (for example, a proxy error page).
    case server(status: Int, code: String, message: String, requestID: String?)
    /// The request never produced an HTTP response (offline, timeout, TLS failure).
    case transport(URLError.Code)
    /// The response wasn't HTTP.
    case invalidResponse
    /// A 2xx response whose body didn't match the expected type.
    case decoding(type: String)

    /// Builds a `.server` error from an error response body.
    init(status: Int, body: Data, fallbackRequestID: String?) {
        if let envelope = try? JSONDecoder().decode(ErrorEnvelope.self, from: body) {
            self = .server(
                status: status,
                code: envelope.error.code,
                message: envelope.error.message,
                requestID: envelope.error.requestId ?? fallbackRequestID)
        } else {
            self = .server(
                status: status,
                code: "http_\(status)",
                message: HTTPURLResponse.localizedString(forStatusCode: status),
                requestID: fallbackRequestID)
        }
    }

    var status: Int? {
        if case .server(let status, _, _, _) = self { return status }
        return nil
    }

    var code: String? {
        if case .server(_, let code, _, _) = self { return code }
        return nil
    }

    /// `401`: the credential was rejected (`token_expired` or `unauthenticated`).
    var isUnauthorized: Bool { status == 401 }

    private struct ErrorEnvelope: Decodable {
        struct Body: Decodable {
            let code: String
            let message: String
            let requestId: String?
        }

        let error: Body
    }
}

extension APIError: LocalizedError {
    nonisolated var errorDescription: String? {
        switch self {
        case .server(let status, let code, _, _):
            switch code {
            case "rate_limited":
                String(localized: "Too many attempts. Wait a minute and try again.")
            case "provider_unavailable":
                String(localized: "That sign-in method isn't available right now. Try again later.")
            case "unauthenticated", "token_expired":
                String(localized: "Your sign-in couldn't be verified. Try again.")
            default:
                status >= 500
                    ? String(localized: "The server ran into a problem. Try again shortly.")
                    : String(localized: "Something went wrong (\(code)).")
            }
        case .transport(.notConnectedToInternet), .transport(.networkConnectionLost),
            .transport(.dataNotAllowed):
            String(localized: "You appear to be offline. Check your connection and try again.")
        case .transport:
            String(localized: "Couldn't reach the server. Check your connection and try again.")
        case .invalidResponse, .decoding:
            String(localized: "The server sent an unexpected response.")
        }
    }
}
