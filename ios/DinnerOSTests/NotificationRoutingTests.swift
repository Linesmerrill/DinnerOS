import Foundation
import SwiftUI
import Testing

@testable import DinnerOS

/// Every notification the API can create opens a screen, and the bell and a tapped push open
/// the same one. The table is `api/internal/notifications/testdata/routes.json`, which the API's
/// tests check against every notification it creates, so a new server type without an app
/// route fails here.
struct NotificationRoutingTests {
    private struct Route: Decodable {
        let type: String
        let subjectKind: String
        let destination: String
    }

    private static func routes() throws -> [Route] {
        let url = URL(fileURLWithPath: #filePath)
            .deletingLastPathComponent()  // DinnerOSTests
            .deletingLastPathComponent()  // ios
            .deletingLastPathComponent()  // repository
            .appending(path: "api/internal/notifications/testdata/routes.json")
        struct Document: Decodable { let routes: [Route] }
        let routes = try JSONDecoder().decode(Document.self, from: Data(contentsOf: url)).routes
        #expect(!routes.isEmpty)
        return routes
    }

    /// A subject ID the app can read for each kind.
    private static func sampleID(kind: String) -> String {
        switch kind {
        case AppNotificationSubject.shoppingWeekKind: "2026-W38"
        case AppNotificationSubject.pantryItemKind: "item-butter"
        default: "job-1"
        }
    }

    private static func notification(type: String, kind: String, id: String) -> AppNotification {
        AppNotification(
            id: "n-1", householdID: "household-1", type: AppNotificationType(rawValue: type), title: "Title",
            body: "Body", subject: AppNotificationSubject(kind: kind, id: id), read: false, createdAt: .now)
    }

    @Test func everyServerTypeHasItsRoute() throws {
        for route in try Self.routes() {
            let id = Self.sampleID(kind: route.subjectKind)
            let destination = NotificationRouting.destination(
                for: Self.notification(type: route.type, kind: route.subjectKind, id: id))
            #expect(destination.fixtureName == route.destination, "\(route.type) opens \(destination)")
            #expect(destination != .notifications, "\(route.type) has no screen")
        }
    }

    @Test func appKnowsExactlyTheServerTypes() throws {
        let known: Set<AppNotificationType> = [
            .pantryLow, .pantryThaw, .shoppingOrderDue, .recipeImportFinished, .recipeImportAttention,
        ]
        let server = Set(try Self.routes().map { AppNotificationType(rawValue: $0.type) })
        #expect(known == server)
    }

    @Test func destinationsCarryTheirSubject() throws {
        let week = try #require(ISOWeek("2026-W38"))
        #expect(
            NotificationRouting.destination(type: .pantryLow, subject: .init(kind: "pantry_item", id: "item-butter"))
                == .pantryItem(id: "item-butter"))
        #expect(
            NotificationRouting.destination(type: .pantryThaw, subject: .init(kind: "pantry_item", id: "item-pork"))
                == .pantryThaw(itemID: "item-pork"))
        #expect(
            NotificationRouting.destination(
                type: .shoppingOrderDue, subject: .init(kind: "shopping_week", id: "2026-W38"))
                == .shopWeek(week))
        #expect(
            NotificationRouting.destination(
                type: .recipeImportAttention, subject: .init(kind: "recipe_import_job", id: "job-1"))
                == .recipeImport)
    }

    /// A tapped push and its bell row are read by the same function, from the same payload.
    @Test func tappedPushOpensWhatTheBellOpens() throws {
        for route in try Self.routes() {
            let id = Self.sampleID(kind: route.subjectKind)
            let push = try #require(
                PushRoute(userInfo: [
                    "aps": ["alert": ["title": "Title"]],
                    "notificationId": "n-1", "householdId": "household-1", "type": route.type,
                    "subject": ["kind": route.subjectKind, "id": id],
                ]))
            let row = Self.notification(type: route.type, kind: route.subjectKind, id: id)
            #expect(NotificationRouting.destination(for: push) == NotificationRouting.destination(for: row))
        }
    }

    @Test func unreadableSubjectsOpenTheNearestScreen() {
        // An order reminder whose week can't be read still opens Shop.
        #expect(
            NotificationRouting.destination(type: .shoppingOrderDue, subject: .init(kind: "shopping_week", id: "soon"))
                == .shop)
        #expect(NotificationRouting.destination(type: .shoppingOrderDue, subject: nil) == .shop)
        #expect(NotificationRouting.destination(type: .pantryLow, subject: nil) == .pantry)
        #expect(NotificationRouting.destination(type: .pantryThaw, subject: nil) == .pantry)
        // A type this build doesn't know opens by its subject, and without one, the bell.
        let future = AppNotificationType(rawValue: "pantry.expiring")
        #expect(
            NotificationRouting.destination(type: future, subject: .init(kind: "pantry_item", id: "item-milk"))
                == .pantryItem(id: "item-milk"))
        #expect(
            NotificationRouting.destination(
                type: AppNotificationType(rawValue: "household.joined"), subject: .init(kind: "member", id: "u-2"))
                == .notifications)
    }

    @Test func goneSubjectsResolveToTheirTab() async {
        let gone = NotificationSubjectCheck(pantryItemExists: { _ in false }, canOpenRecipeImport: { false })
        #expect(await NotificationRouting.resolve(.pantryItem(id: "item-gone"), check: gone) == .pantry)
        #expect(await NotificationRouting.resolve(.recipeImport, check: gone) == .household)
        // Nothing to check: the thaw section and Shop are always there.
        #expect(
            await NotificationRouting.resolve(.pantryThaw(itemID: "item-gone"), check: gone)
                == .pantryThaw(itemID: "item-gone"))

        let present = NotificationSubjectCheck(pantryItemExists: { _ in true }, canOpenRecipeImport: { true })
        #expect(
            await NotificationRouting.resolve(.pantryItem(id: "item-1"), check: present) == .pantryItem(id: "item-1"))
        #expect(await NotificationRouting.resolve(.recipeImport, check: present) == .recipeImport)
    }

    @Test func routerSwitchesTabsAndPushesTheScreen() async throws {
        let tabs = TabRouter()
        var shownWeeks: [ISOWeek] = []
        tabs.showWeek = { shownWeeks.append($0) }

        await tabs.open(.pantryItem(id: "item-butter"))
        #expect(tabs.selection == .pantry)
        #expect(tabs.pantryPath == NavigationPath([PantryItemRoute(itemID: "item-butter")]))

        await tabs.open(.recipeImport)
        #expect(tabs.selection == .household)
        #expect(tabs.householdPath == NavigationPath([RecipeImportRoute()]))

        let week = try #require(ISOWeek("2026-W38"))
        await tabs.open(.shopWeek(week))
        #expect(tabs.selection == .shop)
        #expect(shownWeeks == [week])

        // The thaw reminder opens the Pantry list, where Take Out to Thaw is the first section.
        await tabs.open(.pantryThaw(itemID: "item-pork"))
        #expect(tabs.selection == .pantry)
        #expect(tabs.pantryPath.isEmpty)

        #expect(!tabs.showsNotifications)
        await tabs.open(.notifications)
        #expect(tabs.showsNotifications)
    }

    @Test func routerFallsBackWhenTheSubjectIsGone() async {
        let tabs = TabRouter()
        tabs.check = NotificationSubjectCheck(pantryItemExists: { _ in false }, canOpenRecipeImport: { false })
        tabs.householdPath = NavigationPath([RecipeImportRoute()])

        #expect(await tabs.open(.pantryItem(id: "item-gone")) == .pantry)
        #expect(tabs.selection == .pantry)
        #expect(tabs.pantryPath.isEmpty)

        #expect(await tabs.open(.recipeImport) == .household)
        #expect(tabs.selection == .household)
        #expect(tabs.householdPath.isEmpty)
    }

    @Test func everyRowSaysWhereItGoes() throws {
        var hints: Set<String> = []
        for route in try Self.routes() {
            let id = Self.sampleID(kind: route.subjectKind)
            let destination = NotificationRouting.destination(
                for: Self.notification(type: route.type, kind: route.subjectKind, id: id))
            #expect(destination.accessibilityHint.hasPrefix("Opens"), "\(route.type): \(destination.accessibilityHint)")
            hints.insert(destination.accessibilityHint)
        }
        // Four screens, four different hints.
        #expect(hints.count == Set(try Self.routes().map(\.destination)).count)
    }
}
