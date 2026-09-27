import PhotosUI
import SwiftUI

/// "Import Prices from Order Screenshots…": reads screenshots of the Walmart app's order
/// details on this iPhone, matches the items to the week's lines, and saves only the prices the
/// member reviews. The images are held in memory until the sheet closes — so the review can show
/// the part of a screenshot each item was read from — and are never written to disk or uploaded.
struct OrderImportSheet: View {
    @Environment(ShoppingStore.self) private var shopping
    @Environment(HouseholdStore.self) private var households
    @Environment(\.dismiss) private var dismiss

    @State private var selection: [PhotosPickerItem] = []
    @State private var isReading = false
    @State private var draft: OrderImportDraft?
    /// The picked screenshots, kept only while this sheet is open.
    @State private var screenshots: [Data] = []
    @State private var usedModel = false
    @State private var isSaving = false
    @State private var errorMessage: String?

    var body: some View {
        NavigationStack {
            Group {
                if let draft = Binding($draft) {
                    OrderImportReview(
                        draft: draft, screenshots: screenshots, canSaveTotal: shopping.canEdit, usedModel: usedModel)
                } else {
                    chooser
                }
            }
            .navigationTitle("Import Prices")
            .navigationBarTitleDisplayMode(.inline)
            .toolbar {
                ToolbarItem(placement: .cancellationAction) {
                    Button("Cancel") { dismiss() }
                }
                if let draft {
                    ToolbarItem(placement: .confirmationAction) {
                        Button("Save") { save(draft) }
                            .disabled(!draft.canSave || isSaving)
                    }
                }
            }
            .safeAreaInset(edge: .top, spacing: 0) {
                if draft != nil, let errorMessage {
                    FormErrorLabel(message: errorMessage)
                        .font(.footnote)
                        .padding()
                        .frame(maxWidth: .infinity, alignment: .leading)
                        .background(.bar)
                }
            }
            .disabled(isSaving)
            .interactiveDismissDisabled(isSaving || isReading)
        }
        .onChange(of: selection) {
            guard !selection.isEmpty else { return }
            read(selection)
        }
    }

    private var chooser: some View {
        List {
            Section {
                Text(
                    "In the Walmart app, open the order details or your cart, then take screenshots from the first item to the total."
                )
                if isReading {
                    HStack(spacing: 8) {
                        ProgressView()
                        Text("Reading screenshots…")
                            .foregroundStyle(.secondary)
                    }
                    .accessibilityElement(children: .combine)
                } else {
                    PhotosPicker(
                        selection: $selection, maxSelectionCount: 12, selectionBehavior: .ordered, matching: .images
                    ) {
                        Label("Choose Screenshots", systemImage: "photo.on.rectangle")
                    }
                }
                if let errorMessage {
                    FormErrorLabel(message: errorMessage)
                }
            } footer: {
                Text(
                    "Screenshots are read on this iPhone and never uploaded. Only the prices you save leave your phone."
                )
            }
        }
    }

    private func read(_ items: [PhotosPickerItem]) {
        let lines = shopping.priceableLines
        Task {
            isReading = true
            errorMessage = nil
            defer {
                isReading = false
                selection = []
            }
            do {
                var images: [Data] = []
                for item in items {
                    if let data = try await item.loadTransferable(type: Data.self) {
                        images.append(data)
                    }
                }
                let result = try await OrderScreenshotReader().read(images: images)
                guard !result.order.items.isEmpty || result.order.totalCents != nil else {
                    errorMessage = String(
                        localized: "No items with prices were found. Try screenshots of the order details.")
                    return
                }
                usedModel = result.usedModel
                var newDraft = OrderImportDraft(order: result.order, lines: lines)
                if !shopping.canEdit {
                    newDraft.savesTotal = false
                }
                screenshots = images
                draft = newDraft
            } catch is CancellationError {
                return
            } catch {
                errorMessage = error.localizedDescription
            }
        }
    }

    private func save(_ draft: OrderImportDraft) {
        Task {
            isSaving = true
            defer { isSaving = false }
            errorMessage = nil
            do {
                let prices = draft.prices
                if !prices.isEmpty {
                    try await shopping.savePrices(prices)
                }
                if let total = draft.totalToSave, shopping.canEdit {
                    try await shopping.setOrderTotal(total)
                }
                dismiss()
            } catch is CancellationError {
                return
            } catch {
                errorMessage = ShopErrors.message(for: error, households: households)
            }
        }
    }
}

/// The review before saving: matched items, items without a line, lines without a price, and
/// the order total.
private struct OrderImportReview: View {
    @Binding var draft: OrderImportDraft
    let screenshots: [Data]
    let canSaveTotal: Bool
    let usedModel: Bool
    @State private var viewing: ParsedOrderItem?

    var body: some View {
        List {
            if let total = draft.detectedTotalCents, canSaveTotal {
                Section {
                    Toggle(isOn: $draft.savesTotal) {
                        LabeledContent("Order total", value: MoneyText.format(total))
                    }
                } footer: {
                    Text("Order total (with fees, tax, and tip), saved for the week.")
                }
            }
            let matched = draft.matchedRows
            if !matched.isEmpty {
                Section {
                    ForEach(matched) { row in
                        ImportRow(row: row, lines: draft.lines, draft: $draft, onView: viewAction(row))
                    }
                } header: {
                    Text("Matched")
                } footer: {
                    Text("Each price is what the whole line came to, all packages together.")
                }
            }
            let unmatched = draft.unmatchedRows
            if !unmatched.isEmpty {
                Section {
                    ForEach(unmatched) { row in
                        ImportRow(row: row, lines: draft.lines, draft: $draft, onView: viewAction(row))
                    }
                } header: {
                    Text("Not Matched")
                } footer: {
                    Text("Pick the item each one is for, or leave it as Don't Use.")
                }
            }
            let unpriced = draft.linesWithoutPrice
            if !unpriced.isEmpty {
                Section {
                    ForEach(unpriced) { priced in
                        VStack(alignment: .leading, spacing: 2) {
                            Text(priced.line.name)
                            Text(priced.line.product.displayName)
                                .font(.subheadline)
                                .foregroundStyle(.secondary)
                        }
                        .accessibilityElement(children: .combine)
                    }
                } header: {
                    Text("Still Without a Price")
                }
            }
            Section {
            } footer: {
                VStack(alignment: .leading, spacing: 4) {
                    if usedModel {
                        Text("Apple Intelligence helped read these screenshots on this iPhone.")
                    }
                    Text("Nothing leaves your phone except the prices and total you save.")
                }
            }
        }
        .sheet(item: $viewing) { item in
            OrderItemSourceView(item: item, screenshot: screenshot(for: item))
        }
    }

    /// Opens the part of the screenshot the row was read from, when it knows where that is.
    private func viewAction(_ row: OrderImportDraft.Row) -> (() -> Void)? {
        guard screenshot(for: row.item) != nil else { return nil }
        let item = row.item
        return { viewing = item }
    }

    private func screenshot(for item: ParsedOrderItem) -> Data? {
        guard let screen = item.source?.screen, screenshots.indices.contains(screen) else { return nil }
        return screenshots[screen]
    }
}

/// The part of the member's own screenshot an item was read from — its photo, title, and price —
/// for a title the Walmart app cut off with "…". Cropped in memory while it's on screen; nothing
/// is saved or uploaded.
private struct OrderItemSourceView: View {
    let item: ParsedOrderItem
    let screenshot: Data?
    @Environment(\.dismiss) private var dismiss
    @State private var crop: UIImage?
    @State private var isLoading = true

    var body: some View {
        NavigationStack {
            ScrollView {
                VStack(alignment: .leading, spacing: 16) {
                    if let crop {
                        Image(uiImage: crop)
                            .resizable()
                            .scaledToFit()
                            .clipShape(RoundedRectangle(cornerRadius: 10))
                            .overlay {
                                RoundedRectangle(cornerRadius: 10).strokeBorder(.separator)
                            }
                            .accessibilityLabel(Text("Your screenshot of \(item.name)"))
                    } else if isLoading {
                        ProgressView()
                            .frame(maxWidth: .infinity, minHeight: 120)
                    } else {
                        Text("This part of the screenshot couldn't be shown.")
                            .foregroundStyle(.secondary)
                    }
                    VStack(alignment: .leading, spacing: 4) {
                        Text("Read as")
                            .font(.caption)
                            .foregroundStyle(.secondary)
                        Text(item.name)
                            .textSelection(.enabled)
                        Text(MoneyText.format(item.priceCents))
                            .font(.subheadline)
                            .foregroundStyle(.secondary)
                            .monospacedDigit()
                    }
                    .accessibilityElement(children: .combine)
                    Text("Shown only while you review. It isn't saved or uploaded.")
                        .font(.footnote)
                        .foregroundStyle(.secondary)
                }
                .padding()
            }
            .navigationTitle("From Your Screenshot")
            .navigationBarTitleDisplayMode(.inline)
            .toolbar {
                ToolbarItem(placement: .confirmationAction) {
                    Button("Done") { dismiss() }
                }
            }
        }
        .presentationDetents([.medium, .large])
        .task {
            guard let screenshot, let source = item.source else {
                isLoading = false
                return
            }
            crop = await Task.detached(priority: .userInitiated) {
                OrderScreenshotCrop.image(from: screenshot, source: source)
            }.value
            isLoading = false
        }
    }
}

/// Crops a screenshot to where an item was read, in memory.
nonisolated enum OrderScreenshotCrop {
    static func image(from data: Data, source: OrderItemSource) -> UIImage? {
        guard let image = UIImage(data: data) else { return nil }
        guard let cgImage = upright(image).cgImage else { return nil }
        let rect = source.cropRect(in: CGSize(width: cgImage.width, height: cgImage.height))
        guard !rect.isNull, !rect.isEmpty, let cropped = cgImage.cropping(to: rect) else { return nil }
        return UIImage(cgImage: cropped)
    }

    /// Recognition reads the image the way it's displayed, so the crop has to use the same
    /// orientation. Screenshots are already upright; anything else is redrawn.
    private static func upright(_ image: UIImage) -> UIImage {
        guard image.imageOrientation != .up else { return image }
        let format = UIGraphicsImageRendererFormat()
        format.scale = image.scale
        return UIGraphicsImageRenderer(size: image.size, format: format).image { _ in
            image.draw(in: CGRect(origin: .zero, size: image.size))
        }
    }
}

private struct ImportRow: View {
    let row: OrderImportDraft.Row
    let lines: [PriceableLine]
    @Binding var draft: OrderImportDraft
    /// Shows where on the screenshots the item was read, or `nil` when that isn't known.
    let onView: (() -> Void)?

    var body: some View {
        VStack(alignment: .leading, spacing: 6) {
            HStack(alignment: .firstTextBaseline, spacing: 12) {
                VStack(alignment: .leading, spacing: 2) {
                    // Kept short; the full text is what VoiceOver reads, and View shows the card.
                    Text(row.item.name)
                        .lineLimit(2)
                        .accessibilityLabel(row.item.name)
                        .onTapGesture { onView?() }
                    if let detail {
                        Text(detail)
                            .font(.caption)
                            .foregroundStyle(.secondary)
                    }
                    if let onView {
                        Button(action: onView) {
                            Label("View", systemImage: "text.viewfinder")
                                .font(.caption)
                        }
                        .buttonStyle(.borderless)
                        .accessibilityLabel("View in Screenshot")
                        .accessibilityHint("Shows the part of your screenshot \(row.item.name) was read from.")
                    }
                }
                Spacer(minLength: 8)
                HStack(spacing: 2) {
                    Text(verbatim: "$")
                        .foregroundStyle(.secondary)
                        .accessibilityHidden(true)
                    TextField("Price", text: priceBinding, prompt: Text("Price"))
                        .keyboardType(.decimalPad)
                        .multilineTextAlignment(.trailing)
                        .monospacedDigit()
                        .accessibilityLabel("Price for \(row.item.name)")
                        .accessibilityValue(row.priceCents.map { MoneyText.format($0) } ?? row.priceText)
                }
                .frame(maxWidth: 110)
            }
            Picker("For", selection: lineBinding) {
                Text("Don't Use").tag(PriceableLine.ID?.none)
                ForEach(lines) { priced in
                    Text(priced.line.name).tag(PriceableLine.ID?.some(priced.id))
                }
            }
            .pickerStyle(.menu)
            .font(.subheadline)
            if row.lineID != nil {
                if let confidence = row.confidence {
                    Label(confidence.text, systemImage: confidence == .low ? "questionmark.circle" : "checkmark.circle")
                        .font(.caption)
                        .foregroundStyle(confidence == .low ? AnyShapeStyle(Color.orange) : AnyShapeStyle(.secondary))
                }
                if let error = row.priceError {
                    FormErrorLabel(message: error)
                        .font(.footnote)
                }
            }
        }
    }

    private var detail: String? {
        var parts: [String] = []
        if row.item.quantity > 1 {
            parts.append(String(localized: "Qty \(row.item.quantity)"))
            // The price is the whole line, so show what one of them came to.
            if let cents = row.priceCents {
                parts.append(String(localized: "\(MoneyText.format(cents / row.item.quantity)) each"))
            }
        }
        if row.item.isWeightAdjusted {
            parts.append(String(localized: "Weight-adjusted"))
        }
        switch row.item.status {
        case .substituted: parts.append(String(localized: "Substituted"))
        case .unavailable: parts.append(String(localized: "Unavailable"))
        case .refunded: parts.append(String(localized: "Refunded"))
        case .ordered: break
        }
        return parts.isEmpty ? nil : parts.joined(separator: " · ")
    }

    private var priceBinding: Binding<String> {
        let id = row.id
        return Binding(get: { row.priceText }, set: { draft.setPriceText($0, for: id) })
    }

    private var lineBinding: Binding<PriceableLine.ID?> {
        let id = row.id
        return Binding(get: { row.lineID }, set: { draft.assign(id, to: $0) })
    }
}
