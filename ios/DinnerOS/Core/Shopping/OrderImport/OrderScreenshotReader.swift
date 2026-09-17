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
        var rows: [RecognizedRow] = []
        for (index, image) in images.enumerated() {
            try Task.checkCancellation()
            rows += try await Self.recognizeRows(in: image).map { $0.movedDown(byScreens: index) }
        }
        guard !rows.isEmpty else { throw ReadError.noText }
        let lines = rows.map(\.text)
        let style = OrderScreenshotParser.priceStyle(rows: rows)
        let parsed = OrderScreenshotParser.parse(rows: rows, style: style)
        if let generated = await Self.structureWithModel(lines: lines) {
            let merged = OrderImportMerge.merge(
                parsed: parsed, modelItems: generated.items, modelTotal: generated.total, lines: lines, style: style)
            let usedModel = merged.items != parsed.items
            Self.logger.info(
                "Order import read \(merged.items.count, privacy: .public) items; model used: \(usedModel, privacy: .public)"
            )
            return Result(order: merged, usedModel: usedModel)
        }
        Self.logger.info("Order import read \(parsed.items.count, privacy: .public) items without the model")
        return Result(order: parsed, usedModel: false)
    }

    /// Accurate recognition with language correction, one row per visual row, top to bottom:
    /// text on the same row (a name and its price) is joined left to right, and each row keeps
    /// where it sits so the parser can tell a price above a name from one below it.
    static func recognizeRows(in image: Data) async throws -> [RecognizedRow] {
        var request = RecognizeTextRequest()
        request.recognitionLevel = .accurate
        request.usesLanguageCorrection = true
        let observations = try await request.perform(on: image)
        let pieces: [RecognizedPiece] = observations.compactMap { observation in
            guard let text = observation.topCandidates(1).first?.string else { return nil }
            return RecognizedPiece(text, topDown(observation.boundingBox.cgRect))
        }
        return OrderTextLayout.rows(pieces)
    }

    /// Vision's normalized box, which starts at the bottom left, with the origin at the top left.
    static func topDown(_ rect: CGRect) -> CGRect {
        CGRect(x: rect.minX, y: 1 - rect.maxY, width: rect.width, height: rect.height)
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
