import SwiftUI

@main struct LesterApp: App {
    @State private var model = AppModel()
    var body: some Scene {
        WindowGroup { RootView(model: model).tint(Color(red: 0.17, green: 0.31, blue: 0.23)) }
    }
}
