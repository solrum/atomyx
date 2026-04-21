import SwiftUI

@main
struct AtomyxHostApp: App {
    var body: some Scene {
        WindowGroup {
            VStack(spacing: 8) {
                Text("Atomyx driver").font(.title).bold()
                Text("Host app for the XCUITest bundle. Do not interact.")
                    .font(.caption)
                    .foregroundStyle(.secondary)
                    .multilineTextAlignment(.center)
                    .padding()
            }
        }
    }
}
