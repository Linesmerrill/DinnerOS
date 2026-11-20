import PhotosUI
import SwiftUI

/// Choose Photo: search the web for the dish, already filled in with the recipe's name, or pick
/// from the phone's library. Web photos come from our server, which keeps the search key. When
/// the server has no photo search, only the library shows, so nothing fails or comes back empty.
struct RecipePhotoChooser: View {
    let initialQuery: String
    var searchAvailable = true
    let search: (String, Int) async throws -> RecipePhotoSearchPage
    let onPicked: (UIImage) -> Void
    let onFound: (RecipeFoundPhoto) -> Void

    @Environment(\.dismiss) private var dismiss
    @State private var place: Place = .web
    @State private var libraryItem: PhotosPickerItem?
    @State private var libraryError: String?

    enum Place: Hashable {
        case web
        case library
    }

    var body: some View {
        NavigationStack {
            VStack(spacing: 0) {
                if searchAvailable {
                    Picker("Where", selection: $place) {
                        Text("Search the Web").tag(Place.web)
                        Text("Photo Library").tag(Place.library)
                    }
                    .pickerStyle(.segmented)
                    .padding(.horizontal)
                    .padding(.vertical, DS.Space.s)
                }
                switch shownPlace {
                case .web:
                    RecipePhotoSearchView(initialQuery: initialQuery, search: search, onFound: onFound)
                case .library:
                    library
                }
            }
            .navigationTitle("Choose a Photo")
            .navigationBarTitleDisplayMode(.inline)
            .toolbar {
                ToolbarItem(placement: .cancellationAction) {
                    Button("Cancel") { dismiss() }
                }
            }
        }
    }

    /// The library alone when web search is off.
    var shownPlace: Place { searchAvailable ? place : .library }

    private var library: some View {
        VStack(spacing: 0) {
            if let libraryError {
                Label(libraryError, systemImage: "exclamationmark.triangle")
                    .font(.footnote)
                    .foregroundStyle(.secondary)
                    .padding(DS.Space.s)
            }
            PhotosPicker(selection: $libraryItem, matching: .images, photoLibrary: .shared()) {
                Text("Photo Library")
            }
            .photosPickerStyle(.inline)
            .photosPickerDisabledCapabilities([.selectionActions])
            .photosPickerAccessoryVisibility(.hidden, edges: .all)
            .ignoresSafeArea(edges: .bottom)
        }
        .onChange(of: libraryItem) { _, item in
            guard let item else { return }
            Task {
                let data = try? await item.loadTransferable(type: Data.self)
                libraryItem = nil
                if let data, let image = UIImage(data: data) {
                    onPicked(image)
                } else {
                    libraryError = String(localized: "Couldn't open that photo. Try another one.")
                }
            }
        }
    }
}

/// The web search: a search box, then a grid that loads more as it's scrolled.
struct RecipePhotoSearchView: View {
    let initialQuery: String
    let search: (String, Int) async throws -> RecipePhotoSearchPage
    let onFound: (RecipeFoundPhoto) -> Void

    @State private var query: String
    @State private var searched = ""
    @State private var photos: [RecipeFoundPhoto] = []
    @State private var nextPage: Int?
    @State private var isLoading = false
    @State private var errorMessage: String?
    @State private var sourceName: String?
    @State private var sourceURL: URL?
    @FocusState private var searchFocused: Bool

    init(
        initialQuery: String, search: @escaping (String, Int) async throws -> RecipePhotoSearchPage,
        onFound: @escaping (RecipeFoundPhoto) -> Void
    ) {
        self.initialQuery = initialQuery
        self.search = search
        self.onFound = onFound
        _query = State(initialValue: initialQuery.trimmingCharacters(in: .whitespaces))
    }

    private let columns = [GridItem(.adaptive(minimum: 104), spacing: DS.Space.xxs)]

    var body: some View {
        VStack(spacing: 0) {
            HStack(spacing: DS.Space.s) {
                Image(systemName: "magnifyingglass")
                    .foregroundStyle(.secondary)
                    .accessibilityHidden(true)
                TextField("Search for a photo", text: $query)
                    .submitLabel(.search)
                    .focused($searchFocused)
                    .autocorrectionDisabled()
                    .onSubmit { Task { await start() } }
                if !query.isEmpty {
                    Button {
                        query = ""
                        searchFocused = true
                    } label: {
                        Image(systemName: "xmark.circle.fill")
                            .foregroundStyle(.secondary)
                    }
                    .buttonStyle(.plain)
                    .accessibilityLabel("Clear search")
                }
            }
            .padding(.horizontal, DS.Space.m)
            .frame(minHeight: DS.Size.tapTarget)
            .background(DS.Colors.subtleFill, in: .rect(cornerRadius: DS.Radius.medium))
            .padding(.horizontal)
            .padding(.bottom, DS.Space.s)
            ScrollView {
                if let errorMessage, photos.isEmpty {
                    message(errorMessage, systemImage: "wifi.exclamationmark", retry: true)
                } else if photos.isEmpty, !isLoading, !searched.isEmpty {
                    message(
                        String(localized: "No photos for \"\(searched)\". Try other words."),
                        systemImage: "photo.on.rectangle", retry: false)
                } else if photos.isEmpty, !isLoading {
                    message(
                        String(localized: "Type what's in the dish, like brownies or chicken tacos."),
                        systemImage: "magnifyingglass", retry: false)
                }
                LazyVGrid(columns: columns, spacing: DS.Space.xxs) {
                    ForEach(photos) { photo in
                        Button {
                            onFound(photo)
                        } label: {
                            DS.Colors.fill
                                .aspectRatio(1, contentMode: .fit)
                                .overlay {
                                    AsyncImage(url: URL(string: photo.thumbnailURL)) { image in
                                        image.resizable().scaledToFill()
                                    } placeholder: {
                                        ProgressView()
                                    }
                                }
                                .overlay(alignment: .bottomLeading) {
                                    // Every photo names who took it (Unsplash's guidelines).
                                    if let name = photo.credit {
                                        Text(name)
                                            .font(.caption2)
                                            .lineLimit(1)
                                            .foregroundStyle(.white)
                                            .padding(.horizontal, DS.Space.xxs)
                                            .padding(.vertical, DS.Space.hairline)
                                            .frame(maxWidth: .infinity, alignment: .leading)
                                            .background(.black.opacity(0.45))
                                    }
                                }
                                .clipped()
                                .contentShape(.rect)
                        }
                        .buttonStyle(.plain)
                        .accessibilityLabel(
                            photo.photoCredit(source: sourceName, sourceURL: sourceURL)?.text
                                ?? photo.credit.map { String(localized: "Photo by \($0)") }
                                ?? String(localized: "Photo")
                        )
                        .onAppear {
                            if photo.id == photos.last?.id { Task { await more() } }
                        }
                    }
                }
                .padding(.horizontal, DS.Space.xxs)
                if isLoading {
                    ProgressView()
                        .padding(DS.Space.l)
                }
                if !photos.isEmpty, let sourceName {
                    Group {
                        if let sourceURL {
                            Link(String(localized: "Photos from \(sourceName)"), destination: sourceURL)
                        } else {
                            Text("Photos from \(sourceName)")
                                .foregroundStyle(.secondary)
                        }
                    }
                    .font(.footnote)
                    .padding(DS.Space.l)
                    .accessibilityIdentifier("PhotoSource")
                }
            }
            .scrollDismissesKeyboard(.immediately)
        }
        .task {
            if photos.isEmpty, !query.isEmpty { await start() } else if query.isEmpty { searchFocused = true }
        }
    }

    private func message(_ text: String, systemImage: String, retry: Bool) -> some View {
        VStack(spacing: DS.Space.m) {
            Image(systemName: systemImage)
                .font(.largeTitle)
                .foregroundStyle(.secondary)
                .accessibilityHidden(true)
            Text(text)
                .multilineTextAlignment(.center)
                .foregroundStyle(.secondary)
            if retry {
                Button("Try Again") { Task { await start() } }
                    .buttonStyle(.dsSecondary(.small))
            }
        }
        .padding(DS.Space.xxxl)
        .frame(maxWidth: .infinity)
    }

    private func start() async {
        let text = query.trimmingCharacters(in: .whitespaces)
        guard !text.isEmpty else { return }
        searched = text
        photos = []
        nextPage = 1
        errorMessage = nil
        await more()
    }

    private func more() async {
        guard let page = nextPage, !isLoading else { return }
        isLoading = true
        defer { isLoading = false }
        let text = searched
        do {
            let result = try await search(text, page)
            guard text == searched else { return }
            let known = Set(photos.map(\.id))
            photos += result.photos.filter { !known.contains($0.id) }
            nextPage = result.nextPage
            if let name = result.sourceName {
                sourceName = name
                sourceURL = result.sourceURL
            }
        } catch is CancellationError {
            return
        } catch {
            errorMessage = HouseholdStore.message(for: error)
            nextPage = page
        }
    }
}
