import Foundation

/// A field in a partial update that the API reads three ways: key absent keeps the stored
/// value, `null` clears it, and a value sets it.
nonisolated enum FieldChange<Value: Encodable & Equatable & Sendable>: Equatable, Sendable {
    case keep
    case clear
    case set(Value)
}

nonisolated extension KeyedEncodingContainer {
    /// Omits the key for `.keep`, writes `null` for `.clear`, and the value for `.set`.
    mutating func encodeChange<Value>(_ change: FieldChange<Value>, forKey key: Key) throws {
        switch change {
        case .keep: return
        case .clear: try encodeNil(forKey: key)
        case .set(let value): try encode(value, forKey: key)
        }
    }
}
