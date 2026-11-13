import SwiftUI
import UIKit
import UniformTypeIdentifiers

/// "DinnerOS" in Safari's share sheet: keeps the recipe page's link for the app, which reads
/// the recipe and shows it for checking the next time it opens. Nothing is read or sent from
/// here.
final class ShareViewController: UIViewController {
    private let model = ShareModel()

    override func viewDidLoad() {
        super.viewDidLoad()
        let host = UIHostingController(rootView: ShareView(model: model) { [weak self] in self?.finish() })
        addChild(host)
        host.view.frame = view.bounds
        host.view.autoresizingMask = [.flexibleWidth, .flexibleHeight]
        view.addSubview(host.view)
        host.didMove(toParent: self)
        Task { await readSharedLink() }
    }

    private func finish() {
        extensionContext?.completeRequest(returningItems: nil)
    }

    /// The page's link: a shared URL, or the first link in shared text.
    private func readSharedLink() async {
        let providers = (extensionContext?.inputItems as? [NSExtensionItem] ?? []).flatMap { $0.attachments ?? [] }
        var link: URL?
        for provider in providers where link == nil {
            if provider.hasItemConformingToTypeIdentifier(UTType.url.identifier) {
                link = try? await provider.loadItem(forTypeIdentifier: UTType.url.identifier) as? URL
            } else if provider.hasItemConformingToTypeIdentifier(UTType.plainText.identifier),
                let text = try? await provider.loadItem(forTypeIdentifier: UTType.plainText.identifier) as? String
            {
                link = SharedRecipeLink.webLink(in: text)
            }
        }
        if let link, link.scheme == "https" || link.scheme == "http", SharedRecipeLink.save(link) {
            model.state = .saved(host: link.host() ?? link.absoluteString)
        } else {
            model.state = .noLink
        }
    }
}

@Observable
final class ShareModel {
    enum State: Equatable {
        case reading
        case saved(host: String)
        case noLink
    }

    var state: State = .reading
}

struct ShareView: View {
    let model: ShareModel
    let done: () -> Void

    var body: some View {
        NavigationStack {
            VStack(spacing: 16) {
                switch model.state {
                case .reading:
                    ProgressView()
                case .saved(let host):
                    Image(systemName: "checkmark.circle.fill")
                        .font(.system(size: 48))
                        .foregroundStyle(.green)
                        .accessibilityHidden(true)
                    Text("Recipe saved for DinnerOS")
                        .font(.title3.bold())
                    Text("Open DinnerOS to check the ingredients from \(host) and add it to your recipes.")
                        .multilineTextAlignment(.center)
                        .foregroundStyle(.secondary)
                case .noLink:
                    Image(systemName: "link.badge.plus")
                        .font(.system(size: 44))
                        .foregroundStyle(.secondary)
                        .accessibilityHidden(true)
                    Text("No recipe link here")
                        .font(.title3.bold())
                    Text("Share a recipe page from Safari or another browser.")
                        .multilineTextAlignment(.center)
                        .foregroundStyle(.secondary)
                }
            }
            .padding(24)
            .frame(maxWidth: .infinity, maxHeight: .infinity)
            .toolbar {
                ToolbarItem(placement: .confirmationAction) {
                    Button("Done", action: done)
                }
            }
            .navigationTitle("DinnerOS")
            .navigationBarTitleDisplayMode(.inline)
        }
    }
}
