import PhotosUI
import SwiftUI
import UIKit

/// A recipe's own photo: take one, pick one from the library, change it, or remove it. The
/// photo is sent as soon as it's picked, so saving the recipe only has to carry its link.
struct RecipePhotoSection: View {
    @Binding var imageURL: String?
    /// Sends a JPEG and returns its link.
    let upload: (Data) async throws -> String

    @State private var pickerItem: PhotosPickerItem?
    @State private var showsCamera = false
    @State private var isUploading = false
    @State private var errorMessage: String?
    /// The picked photo, shown while it uploads and after, so there's no wait to see it.
    @State private var preview: UIImage?

    private var hasPhoto: Bool { imageURL != nil || preview != nil }

    private var cameraAvailable: Bool { UIImagePickerController.isSourceTypeAvailable(.camera) }

    var body: some View {
        Section {
            if preview != nil || imageURL != nil {
                photo
                    .listRowInsets(EdgeInsets())
            }
            HStack(spacing: 10) {
                if cameraAvailable {
                    Button {
                        showsCamera = true
                    } label: {
                        Label("Take Photo", systemImage: "camera")
                            .frame(maxWidth: .infinity, minHeight: 44)
                    }
                    .buttonStyle(.bordered)
                }
                let hasPhoto = hasPhoto
                PhotosPicker(selection: $pickerItem, matching: .images, photoLibrary: .shared()) {
                    PickPhotoLabel(hasPhoto: hasPhoto)
                }
                .buttonStyle(.bordered)
            }
            .disabled(isUploading)
            if imageURL != nil || preview != nil {
                Button("Remove Photo", role: .destructive) {
                    imageURL = nil
                    preview = nil
                }
                .disabled(isUploading)
            }
            if let errorMessage {
                Label(errorMessage, systemImage: "exclamationmark.triangle")
                    .font(.footnote)
                    .foregroundStyle(.secondary)
            }
        } header: {
            Text("Photo")
        }
        .onChange(of: pickerItem) { _, item in
            guard let item else { return }
            Task {
                let data = try? await item.loadTransferable(type: Data.self)
                pickerItem = nil
                if let data, let image = UIImage(data: data) {
                    await send(image)
                } else {
                    errorMessage = String(localized: "Couldn't open that photo. Try another one.")
                }
            }
        }
        .fullScreenCover(isPresented: $showsCamera) {
            CameraPicker { image in
                showsCamera = false
                if let image { Task { await send(image) } }
            }
            .ignoresSafeArea()
        }
    }

    @ViewBuilder
    private var photo: some View {
        ZStack {
            if let preview {
                Image(uiImage: preview)
                    .resizable()
                    .scaledToFill()
            } else {
                RecipeImage(url: imageURL.flatMap(URL.init(string:)), pointWidth: 600)
            }
            if isUploading {
                ProgressView()
                    .padding(12)
                    .background(.regularMaterial, in: .circle)
            }
        }
        .frame(maxWidth: .infinity)
        .frame(height: 220)
        .clipped()
        .accessibilityElement()
        .accessibilityLabel(isUploading ? "Adding the photo" : "Recipe photo")
    }

    private func send(_ image: UIImage) async {
        guard let jpeg = RecipePhotoPrep.jpeg(from: image) else {
            errorMessage = String(localized: "Couldn't open that photo. Try another one.")
            return
        }
        let previous = (imageURL, preview)
        preview = image
        isUploading = true
        errorMessage = nil
        defer { isUploading = false }
        do {
            imageURL = try await upload(jpeg)
        } catch is CancellationError {
            (imageURL, preview) = previous
        } catch {
            (imageURL, preview) = previous
            errorMessage = String(localized: "Couldn't add the photo. Check your connection and try again.")
        }
    }
}

/// "Choose Photo", or "Change Photo" once there is one.
private struct PickPhotoLabel: View {
    let hasPhoto: Bool

    var body: some View {
        Label(hasPhoto ? "Change Photo" : "Choose Photo", systemImage: "photo.on.rectangle")
            .frame(maxWidth: .infinity, minHeight: 44)
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
