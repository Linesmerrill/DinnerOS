import Foundation
import Testing

@testable import DinnerOS

/// The server's safe cooking temperatures decode, and an older server without them still works.
struct SafeTemperatureDecodingTests {
    @Test func temperaturesDecodeWithTheirSource() throws {
        let json = """
            {"recipeId":"r1","steps":[],"safeTemperatures":[
              {"name":"Chicken Breasts","fahrenheit":165,"restMinutes":0,"text":"Chicken Breasts: 165°F"},
              {"name":"Pork Chops","fahrenheit":145,"restMinutes":3,"text":"Pork Chops: 145°F, then rest 3 minutes"}],
             "safeTemperaturesSource":"Safe minimum internal temperatures, from the USDA."}
            """
        let instructions = try JSONDecoder().decode(RecipeInstructions.self, from: Data(json.utf8))
        #expect(
            instructions.safeTemperatures.map(\.text) == [
                "Chicken Breasts: 165°F", "Pork Chops: 145°F, then rest 3 minutes",
            ])
        #expect(instructions.safeTemperatures.first?.fahrenheit == 165)
        #expect(instructions.safeTemperaturesSource == "Safe minimum internal temperatures, from the USDA.")
    }

    @Test func anOlderServerHasNone() throws {
        let instructions = try JSONDecoder().decode(
            RecipeInstructions.self, from: Data(#"{"recipeId":"r1","steps":[]}"#.utf8))
        #expect(instructions.safeTemperatures.isEmpty)
        #expect(instructions.safeTemperaturesSource.isEmpty)
    }
}
