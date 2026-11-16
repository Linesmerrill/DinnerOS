import SwiftUI
import UIKit

/// Where a recipe's photo can come from: one the member uploads, or one found on the web.
struct RecipePhotoSource {
    /// Sends a JPEG and returns its link.
    let upload: (Data) async throws -> String
    /// A page of photos on the web for a search.
    let search: (String, Int) async throws -> RecipePhotoSearchPage
    /// Keeps a found photo as the household's own and returns its link.
    let useFound: (String) async throws -> String
    /// Whether to offer Search the Web. Off when the server has no photo search.
    var searchAvailable = true
}

/// The photo being added: what's showing, whether it's sending, and what went wrong. The form
/// owns it and presents the camera and the chooser from the form itself, not from a row in it,
/// so they open every time.
@MainActor @Observable
final class RecipePhotoModel {
    var showsChooser = false
    var showsCamera = false
    private(set) var isUploading = false
    var errorMessage: String?
    /// The picked photo, shown while it uploads and after, so there's no wait to see it.
    private(set) var preview: UIImage?

    var cameraAvailable: Bool { UIImagePickerController.isSourceTypeAvailable(.camera) }

    func hasPhoto(_ imageURL: String?) -> Bool { imageURL != nil || preview != nil }

    func remove(_ imageURL: Binding<String?>) {
        imageURL.wrappedValue = nil
        preview = nil
    }

    /// Sends a photo from the camera or the library.
    func send(_ image: UIImage, to imageURL: Binding<String?>, source: RecipePhotoSource) async {
        guard let jpeg = RecipePhotoPrep.jpeg(from: image) else {
            errorMessage = String(localized: "Couldn't open that photo. Try another one.")
            return
        }
        await replace(imageURL, showing: image) { try await source.upload(jpeg) }
    }

    /// Keeps a photo found on the web.
    func use(_ found: RecipeFoundPhoto, to imageURL: Binding<String?>, source: RecipePhotoSource) async {
        await replace(imageURL, showing: nil) { try await source.useFound(found.id) }
    }

    private func replace(
        _ imageURL: Binding<String?>, showing image: UIImage?, link: () async throws -> String
    ) async {
        let previous = (imageURL.wrappedValue, preview)
        preview = image
        isUploading = true
        errorMessage = nil
        defer { isUploading = false }
        do {
            imageURL.wrappedValue = try await link()
        } catch is CancellationError {
            (imageURL.wrappedValue, preview) = previous
        } catch {
            (imageURL.wrappedValue, preview) = previous
            // A found photo can fail for its own reasons ("isn't there anymore"), which the
            // server says in plain words.
            errorMessage =
                image == nil
                ? HouseholdStore.message(for: error)
                : String(localized: "Couldn't add the photo. Check your connection and try again.")
        }
    }
}

extension View {
    /// The camera and the photo chooser for a recipe form's photo.
    func recipePhotoPickers(
        _ model: RecipePhotoModel, imageURL: Binding<String?>, searchText: String, source: RecipePhotoSource
    ) -> some View {
        modifier(RecipePhotoPickers(model: model, imageURL: imageURL, searchText: searchText, source: source))
    }
}

private struct RecipePhotoPickers: ViewModifier {
    @Bindable var model: RecipePhotoModel
    @Binding var imageURL: String?
    let searchText: String
    let source: RecipePhotoSource

    func body(content: Content) -> some View {
        content
            .sheet(isPresented: $model.showsChooser) {
                RecipePhotoChooser(
                    initialQuery: searchText, searchAvailable: source.searchAvailable, search: source.search,
                    onPicked: { image in
                        model.showsChooser = false
                        Task { await model.send(image, to: $imageURL, source: source) }
                    },
                    onFound: { found in
                        model.showsChooser = false
                        Task { await model.use(found, to: $imageURL, source: source) }
                    })
            }
            .fullScreenCover(isPresented: $model.showsCamera) {
                CameraPicker { image in
                    model.showsCamera = false
                    if let image { Task { await model.send(image, to: $imageURL, source: source) } }
                }
                .ignoresSafeArea()
            }
    }
}

/// A recipe's own photo: take one, choose one from the web or the library, or remove it. The
/// photo is sent as soon as it's picked, so saving the recipe only has to carry its link.
struct RecipePhotoSection: View {
    let model: RecipePhotoModel
    @Binding var imageURL: String?

    var body: some View {
        Section {
            if model.hasPhoto(imageURL) {
                photo
                    .listRowInsets(EdgeInsets())
            }
            if model.cameraAvailable {
                Button {
                    Keyboard.dismiss()
                    model.showsCamera = true
                } label: {
                    Label("Take Photo", systemImage: "camera")
                        .frame(minHeight: 44)
                }
                .disabled(model.isUploading)
            }
            Button {
                Keyboard.dismiss()
                model.showsChooser = true
            } label: {
                Label(
                    model.hasPhoto(imageURL) ? "Change Photo" : "Choose Photo", systemImage: "photo.on.rectangle"
                )
                .frame(minHeight: 44)
            }
            .disabled(model.isUploading)
            if model.hasPhoto(imageURL) {
                Button(role: .destructive) {
                    model.remove($imageURL)
                } label: {
                    Label("Remove Photo", systemImage: "trash")
                        .frame(minHeight: 44)
                }
                .disabled(model.isUploading)
            }
            if let errorMessage = model.errorMessage {
                Label(errorMessage, systemImage: "exclamationmark.triangle")
                    .font(.footnote)
                    .foregroundStyle(.secondary)
            }
        } header: {
            Text("Photo")
        }
    }

    /// The photo across the row at 4:3, the shape recipe photos are shown in elsewhere.
    private var photo: some View {
        Color.clear
            .aspectRatio(4.0 / 3.0, contentMode: .fit)
            .frame(maxWidth: .infinity)
            .overlay {
                if let preview = model.preview {
                    Image(uiImage: preview)
                        .resizable()
                        .scaledToFill()
                } else {
                    RecipeImage(url: imageURL.flatMap(URL.init(string:)), pointWidth: 600)
                }
            }
            .overlay {
                if model.isUploading {
                    ProgressView()
                        .padding(12)
                        .background(.regularMaterial, in: .circle)
                }
            }
            .clipped()
            .accessibilityElement()
            .accessibilityLabel(model.isUploading ? "Adding the photo" : "Recipe photo")
    }
}

/// Getting a photo ready to send: no bigger than it needs to be on a phone or an iPad.
enum RecipePhotoPrep {
    /// The longest side, in pixels.
    static let maxPixels: CGFloat = 1600

    /// The image as a JPEG, scaled down so its longest side is at most `maxPixels`.
    static func jpeg(from image: UIImage, quality: CGFloat = 0.8) -> Data? {
        let size = image.size.applying(CGAffineTransform(scaleX: image.scale, y: image.scale))
        guard size.width > 0, size.height > 0 else { return nil }
        let ratio = min(1, maxPixels / max(size.width, size.height))
        let target = CGSize(width: (size.width * ratio).rounded(), height: (size.height * ratio).rounded())
        let format = UIGraphicsImageRendererFormat.default()
        format.scale = 1
        format.opaque = true
        let resized = UIGraphicsImageRenderer(size: target, format: format).image { _ in
            image.draw(in: CGRect(origin: .zero, size: target))
        }
        return resized.jpegData(compressionQuality: quality)
    }
}

/// The camera, for taking a photo of the dish.
struct CameraPicker: UIViewControllerRepresentable {
    let done: (UIImage?) -> Void

    func makeCoordinator() -> Coordinator { Coordinator(done: done) }

    func makeUIViewController(context: Context) -> UIImagePickerController {
        let picker = UIImagePickerController()
        picker.sourceType = .camera
        picker.delegate = context.coordinator
        return picker
    }

    func updateUIViewController(_ controller: UIImagePickerController, context: Context) {}

    final class Coordinator: NSObject, UIImagePickerControllerDelegate, UINavigationControllerDelegate {
        let done: (UIImage?) -> Void

        init(done: @escaping (UIImage?) -> Void) { self.done = done }

        func imagePickerController(
            _ picker: UIImagePickerController,
            didFinishPickingMediaWithInfo info: [UIImagePickerController.InfoKey: Any]
        ) {
            done(info[.originalImage] as? UIImage)
        }

        func imagePickerControllerDidCancel(_ picker: UIImagePickerController) {
            done(nil)
        }
    }
}
