import Foundation

/// Helpers for additive API fields. A field a newer server added, or one this build can't
/// read, falls back to a default instead of failing the whole response.
nonisolated extension KeyedDecodingContainer {
    /// The value, or `nil` when the key is missing, `null`, or holds something unreadable.
    func decodeLenient<Value: Decodable>(_ type: Value.Type, forKey key: Key) -> Value? {
        (try? decodeIfPresent(Value.self, forKey: key)) ?? nil
    }

    /// The readable elements of an array. Unreadable elements are skipped; a missing,
    /// `null`, or non-array value is empty.
    func decodeLossyArray<Element: Decodable>(_ type: Element.Type, forKey key: Key) -> [Element] {
        decodeLenient(LossyArray<Element>.self, forKey: key)?.elements ?? []
    }

    /// An integer sent as a number or a numeric string, for query-like objects.
    func decodeLenientInt(forKey key: Key) -> Int? {
        if let value = decodeLenient(Int.self, forKey: key) {
            return value
        }
        return decodeLenient(String.self, forKey: key).flatMap { Int($0.trimmingCharacters(in: .whitespaces)) }
    }

    /// A Boolean sent as `true`/`false` or as the strings `"true"`/`"false"`.
    func decodeLenientBool(forKey key: Key) -> Bool? {
        if let value = decodeLenient(Bool.self, forKey: key) {
            return value
        }
        switch decodeLenient(String.self, forKey: key)?.lowercased() {
        case "true": return true
        case "false": return false
        default: return nil
        }
    }
}

/// An array that keeps the elements it can decode.
nonisolated struct LossyArray<Element: Decodable>: Decodable {
    let elements: [Element]

    init(from decoder: any Decoder) throws {
        var container = try decoder.unkeyedContainer()
        var elements: [Element] = []
        while !container.isAtEnd {
            if let element = try? container.decode(Element.self) {
                elements.append(element)
            } else {
                // A failed decode doesn't advance the container, so skip the element explicitly.
                _ = try? container.decode(Skipped.self)
            }
        }
        self.elements = elements
    }

    /// Decodes from any JSON value.
    private struct Skipped: Decodable {
        init(from decoder: any Decoder) throws {}
    }
}
