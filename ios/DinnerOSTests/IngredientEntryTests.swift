import Foundation
import ImageIO
import Testing
import UIKit

@testable import DinnerOS

/// Typing a recipe's ingredients by hand: rows that keep the keyboard, Next to move on, the
/// amount keypad, and the units it offers.
struct IngredientEntryTests {
    // MARK: Row identity (the keyboard used to close after one letter)

    @Test func typingKeepsTheRowsIdentity() {
        var line = RecipeDraftIngredient()
        let id = line.id
        line.name = "G"
        line.name = "Garlic"
        line.quantity = "2"
        line.unit = "clove"
        // The list is drawn by `id`. If it changed with the text, SwiftUI would rebuild the
        // row on every keystroke and the field would lose the keyboard.
        #expect(line.id == id)
    }

    @Test func rowIdentityStaysOffTheWireAndOutOfEquality() throws {
        let a = RecipeDraftIngredient(name: "Garlic", quantity: "2", unit: "clove")
        var b = a
        b.key = UUID()
        #expect(a == b)
        #expect(a.hashValue == b.hashValue)
        let json = try #require(
            try JSONSerialization.jsonObject(with: JSONEncoder().encode(a)) as? [String: Any])
        #expect(json["key"] == nil)
        #expect(json["name"] as? String == "Garlic")
        let decoded = try JSONDecoder().decode(
            RecipeDraftIngredient.self, from: Data(#"{"name":"Rice","pantryStaple":false}"#.utf8))
        #expect(decoded.name == "Rice")
    }

    @Test func eachDecodedLineGetsItsOwnRow() throws {
        let draft = try JSONDecoder().decode(
            RecipeDraft.self,
            from: Data(
                #"{"name":"X","servings":2,"cuisines":[],"tags":[],"steps":[],"warnings":[],"fromUrl":false,"ingredients":[{"name":"Salt","pantryStaple":true},{"name":"Salt","pantryStaple":true}]}"#
                    .utf8))
        // Two identical lines still need two rows.
        #expect(draft.ingredients[0].id != draft.ingredients[1].id)
    }

    // MARK: The amount keypad

    @Test func digitsAndADecimalPoint() {
        var text = ""
        for key: AmountEntry.Key in [.digit(1), .decimal, .digit(5)] { text = AmountEntry.apply(key, to: text) }
        #expect(text == "1.5")
        // One decimal point per number.
        #expect(AmountEntry.apply(.decimal, to: "1.5") == "1.5")
        #expect(AmountEntry.apply(.decimal, to: "") == "0.")
    }

    @Test func fractionKeysMakeMixedNumbers() {
        #expect(AmountEntry.apply(.fraction(1, 2), to: "") == "1/2")
        #expect(AmountEntry.apply(.fraction(1, 2), to: "1") == "1 1/2")
        #expect(AmountEntry.apply(.fraction(3, 4), to: "2.") == "2 3/4")
        // Another fraction swaps the one that's there.
        #expect(AmountEntry.apply(.fraction(1, 3), to: "1 1/2") == "1 1/3")
        // A decimal and a fraction don't mix.
        #expect(AmountEntry.apply(.fraction(1, 2), to: "1.5") == "1.5")
        // Nothing follows a fraction.
        #expect(AmountEntry.apply(.digit(3), to: "1/2") == "1/2")
        #expect(AmountEntry.apply(.decimal, to: "1/2") == "1/2")
    }

    @Test func deleteTakesAFractionInOnePress() {
        #expect(AmountEntry.apply(.delete, to: "1 1/2") == "1")
        #expect(AmountEntry.apply(.delete, to: "1/2") == "")
        #expect(AmountEntry.apply(.delete, to: "12") == "1")
        #expect(AmountEntry.apply(.delete, to: "") == "")
    }

    @Test func fractionsShowAsSymbolsAndSaveAsText() {
        #expect(AmountEntry.display("1 1/2") == "1 ½")
        #expect(AmountEntry.display("3/4") == "¾")
        #expect(AmountEntry.raw("1 ½") == "1 1/2")
        #expect(AmountEntry.raw("1½") == "1 1/2")
        #expect(AmountEntry.raw("⅔") == "2/3")
        for (n, d) in AmountEntry.fractions {
            let text = "2 \(n)/\(d)"
            #expect(AmountEntry.raw(AmountEntry.display(text)) == text)
            // What the keypad makes is a number the save check accepts.
            #expect(RecipeEditDraft.number(text) != nil)
        }
    }

    // MARK: Units

    @Test func quickUnitsAreTheOnesAsked() {
        #expect(RecipeUnit.quick == ["oz", "g", "lb", "cup", "tbsp", "tsp", "can", "bag", "clove", "count"])
        #expect(RecipeUnit.quick.allSatisfy(RecipeUnit.codes.contains))
        #expect(RecipeUnit.label("count") == "each")
        #expect(RecipeUnit.label("tbsp") == "Tbsp")
        #expect(RecipeUnit.label("bag") == "bag")
    }

    // MARK: Next goes on to the next ingredient

    @Test func editNextMovesOnAndMakesANewLineAfterTheLast() throws {
        var draft = RecipeEditDraft(recipe: try recipe(source: "manual"), servings: 2)
        let first = draft.lines[0].id
        let following = draft.line(after: first)
        #expect(following == draft.lines[1].id)
        let last = draft.lines[1].id
        let next = draft.line(after: last)
        let added = try #require(next)
        #expect(draft.lines.count == 3)
        #expect(draft.lines[2].id == added)
        // A blank last line doesn't make another.
        let none = draft.line(after: added)
        #expect(none == nil)
        #expect(draft.lines.count == 3)
    }

    // MARK: Photos on a recipe the household added

    @Test func editSendsThePhotoOnlyWhenItChanged() throws {
        var draft = RecipeEditDraft(recipe: try recipe(source: "manual", image: "https://api.test/p/a"), servings: 2)
        #expect(draft.canChangePhoto)
        #expect(draft.request.imageURL == nil)
        #expect(!draft.hasChanges)
        draft.imageURL = "https://api.test/p/b"
        #expect(draft.hasChanges)
        #expect(draft.request.imageURL == "https://api.test/p/b")
        draft.imageURL = nil
        #expect(draft.request.imageURL == "")
        let body = try #require(
            try JSONSerialization.jsonObject(with: JSONEncoder().encode(draft.request)) as? [String: Any])
        #expect(body["imageUrl"] as? String == "")
        let unchanged = RecipeEditDraft(recipe: try recipe(source: "manual"), servings: 2).request
        let plain = try #require(
            try JSONSerialization.jsonObject(with: JSONEncoder().encode(unchanged)) as? [String: Any])
        #expect(plain["imageUrl"] == nil)
    }

    @Test func onlyRecipesTheHouseholdAddedTakeAPhoto() throws {
        #expect(RecipeEditDraft(recipe: try recipe(source: "manual")).canChangePhoto)
        #expect(RecipeEditDraft(recipe: try recipe(source: "user")).canChangePhoto)
        #expect(!RecipeEditDraft(recipe: try recipe(source: "hellofresh")).canChangePhoto)
    }

    @Test func photosAreScaledDownBeforeSending() throws {
        let big = UIGraphicsImageRenderer(
            size: CGSize(width: 4000, height: 3000),
            format: {
                let f = UIGraphicsImageRendererFormat(); f.scale = 1; return f
            }()
        ).image { context in
            UIColor.orange.setFill()
            context.fill(CGRect(x: 0, y: 0, width: 4000, height: 3000))
        }
        let jpeg = try #require(RecipePhotoPrep.jpeg(from: big))
        let image = try #require(UIImage(data: jpeg))
        #expect(max(image.size.width * image.scale, image.size.height * image.scale) == RecipePhotoPrep.maxPixels)
        #expect(jpeg.starts(with: [0xFF, 0xD8]))
        // The photo is drawn again, so nothing from the camera's metadata (location) comes along.
        let source = try #require(CGImageSourceCreateWithData(jpeg as CFData, nil))
        let properties = CGImageSourceCopyPropertiesAtIndex(source, 0, nil) as? [CFString: Any]
        #expect(properties?[kCGImagePropertyGPSDictionary] == nil)
        // A small photo is left its size.
        let small = UIGraphicsImageRenderer(
            size: CGSize(width: 300, height: 200),
            format: {
                let f = UIGraphicsImageRendererFormat(); f.scale = 1; return f
            }()
        ).image { _ in }
        let smallJPEG = try #require(RecipePhotoPrep.jpeg(from: small))
        #expect(UIImage(data: smallJPEG)?.size == CGSize(width: 300, height: 200))
    }

    @Test func aPhotoIsSentAsAJPEG() throws {
        let request = APIRequest.post(
            "/api/v1/households/h1/recipe-photos", data: Data([0xFF, 0xD8]), contentType: "image/jpeg")
        let client = APIClient(
            baseURL: try #require(URL(string: Fixtures.baseURLString)), transport: StubTransport { _ in (200, Data()) })
        let urlRequest = client.makeURLRequest(for: request, requestID: "r")
        #expect(urlRequest.value(forHTTPHeaderField: "Content-Type") == "image/jpeg")
        #expect(urlRequest.httpBody == Data([0xFF, 0xD8]))
        // Everything else is still JSON.
        let json = try APIRequest.post("/x", body: ["a": 1])
        #expect(
            client.makeURLRequest(for: json, requestID: "r").value(forHTTPHeaderField: "Content-Type")
                == "application/json")
    }

    private func recipe(source: String, image: String? = nil) throws -> Recipe {
        let imageField = image.map { #","imageUrl":"\#($0)""# } ?? ""
        let json = """
            {"id":"r1","householdId":"h1","source":"\(source)","name":"Chili","isAddon":false,"servings":[2]\(imageField),
             "cuisines":[],"tags":[],"orderWeeks":[],"timesOrdered":0,
             "createdAt":"2026-11-01T00:00:00Z","updatedAt":"2026-11-01T00:00:00Z",
             "householdRating":{"count":0,"average":null},
             "ingredients":[
               {"ingredientId":"i-beans","name":"Beans","category":"pantry","pantryStaple":false,"amounts":[
                 {"servings":2,"quantity":"1","quantityValue":1,"unit":"can","sourceUnit":"","rawText":""}]},
               {"ingredientId":"i-salt","name":"Salt","category":"spices","pantryStaple":true,"amounts":[
                 {"servings":2,"quantity":null,"quantityValue":null,"unit":"","sourceUnit":"","rawText":""}]}],
             "steps":[{"index":1,"text":"Cook it."}]}
            """
        return try JSONCoding.makeDecoder().decode(Recipe.self, from: Data(json.utf8))
    }
}
