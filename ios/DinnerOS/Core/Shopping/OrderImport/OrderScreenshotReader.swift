import CoreGraphics
import Foundation
import Vision
import os

#if canImport(FoundationModels)
    import FoundationModels
#endif

/// Reads order screenshots on this device: Vision recognizes the text, Apple Intelligence
/// (when available) structures it, and `OrderScreenshotParser` reads it deterministically and
/// checks the model. Images stay in memory and nothing is sent anywhere.
nonisolated struct OrderScreenshotReader: Sendable {
    nonisolated struct Result: Sendable {
        let order: ParsedOrder
        /// Whether Apple Intelligence's reading was used.
        let usedModel: Bool
    }

    enum ReadError: LocalizedError {
        case noText

        var errorDescription: String? {
            String(localized: "No text was found in those images. Try screenshots of the order details.")
        }
    }

    private static let logger = Logger(subsystem: "DinnerOS", category: "order-import")

    func read(images: [Data]) async throws -> Result {
        var lines: [String] = []
        for image in images {
            try Task.checkCancellation()
            lines += try await Self.recognizeLines(in: image)
        }
        guard !lines.isEmpty else { throw ReadError.noText }
        let parsed = OrderScreenshotParser.parse(lines: lines)
        if let generated = await Self.structureWithModel(lines: lines) {
            let merged = OrderImportMerge.merge(
                parsed: parsed, modelItems: generated.items, modelTotal: generated.total, lines: lines)
            let usedModel = merged.items != parsed.items
            Self.logger.info(
                "Order import read \(merged.items.count, privacy: .public) items; model used: \(usedModel, privacy: .public)"
            )
            return Result(order: merged, usedModel: usedModel)
        }
        Self.logger.info("Order import read \(parsed.items.count, privacy: .public) items without the model")
        return Result(order: parsed, usedModel: false)
    }

    /// Accurate recognition with language correction, one string per visual row: text on the
    /// same row (a name and its price) is joined left to right.
    static func recognizeLines(in image: Data) async throws -> [String] {
        var request = RecognizeTextRequest()
        request.recognitionLevel = .accurate
        request.usesLanguageCorrection = true
        let observations = try await request.perform(on: image)
        let pieces: [(rect: CGRect, text: String)] = observations.compactMap { observation in
            guard let text = observation.topCandidates(1).first?.string else { return nil }
            return (observation.boundingBox.cgRect, text)
        }
        return rows(pieces)
    }

    /// Groups recognized pieces into rows, top to bottom. Vision's coordinates start at the
    /// bottom left.
    static func rows(_ pieces: [(rect: CGRect, text: String)]) -> [String] {
        let sorted = pieces.sorted { $0.rect.midY > $1.rect.midY }
        var rows: [[(rect: CGRect, text: String)]] = []
        for piece in sorted {
            if let last = rows.last?.last,
                abs(last.rect.midY - piece.rect.midY) < max(last.rect.height, piece.rect.height) / 2
            {
                rows[rows.count - 1].append(piece)
            } else {
                rows.append([piece])
            }
        }
        return rows.map { row in row.sorted { $0.rect.minX < $1.rect.minX }.map(\.text).joined(separator: " ") }
    }

    /// The model's items and total, or `nil` when Apple Intelligence isn't available, the text
    /// is too long for it, or it doesn't answer within 20 seconds.
    static func structureWithModel(lines: [String]) async -> (items: [ModelOrderItem], total: String?)? {
        #if canImport(FoundationModels)
            guard #available(iOS 26.0, *) else { return nil }
            return await structureWithSystemModel(lines: lines)
        #else
            return nil
        #endif
    }

    #if canImport(FoundationModels)
        @available(iOS 26.0, *)
        private static func structureWithSystemModel(lines: [String]) async -> (
            items: [ModelOrderItem], total: String?
        )? {
            guard case .available = SystemLanguageModel.default.availability else { return nil }
            let text = lines.joined(separator: "\n")
            // Keep well inside the on-device model's context window.
            guard text.count <= 8_000 else { return nil }
            let session = LanguageModelSession(
                instructions: """
                    You read text recognized from screenshots of a grocery order. List each item that was \
                    bought with the price paid for it and its quantity. Skip was-prices, savings, fees, tax, \
                    tip, and unavailable or refunded items. Copy names and prices exactly as written.
                    """)
            let response = Task { () -> GeneratedOrder? in
                try? await session.respond(to: text, generating: GeneratedOrder.self).content
            }
            let timeout = Task {
                try? await Task.sleep(for: .seconds(20))
                response.cancel()
            }
            let answer = await response.value
            timeout.cancel()
            guard let answer else { return nil }
            let items = answer.items.map { ModelOrderItem(name: $0.name, price: $0.price, quantity: $0.quantity) }
            let total = answer.orderTotal.trimmingCharacters(in: .whitespaces)
            return (items, total.isEmpty ? nil : total)
        }
    #endif
}

#if canImport(FoundationModels)
    @available(iOS 26.0, *)
    @Generable
    nonisolated struct GeneratedOrder {
        @Guide(description: "Every item that was bought, in order")
        var items: [GeneratedOrderItem]
        @Guide(description: "The order total including fees, tax, and tip, such as $130.42, or empty if not shown")
        var orderTotal: String
    }

    @available(iOS 26.0, *)
    @Generable
    nonisolated struct GeneratedOrderItem {
        @Guide(description: "The product name as written")
        var name: String
        @Guide(description: "The price paid for this item, such as $4.98")
        var price: String
        @Guide(description: "How many were bought; 1 when not shown", .range(1...99))
        var quantity: Int
    }
#endif
