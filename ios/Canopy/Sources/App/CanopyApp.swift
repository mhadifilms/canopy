import SwiftUI

@main
struct CanopyApp: App {
    #if os(iOS)
    @UIApplicationDelegateAdaptor(AppDelegate.self) private var appDelegate
    #endif

    @State private var appState = AppState()

    var body: some Scene {
        WindowGroup {
            Group {
                if appState.showOnboarding {
                    NavigationStack {
                        WelcomeView(appState: appState)
                    }
                } else {
                    TabView {
                        SessionListView(appState: appState)
                            .tabItem {
                                Label("Sessions", systemImage: "terminal")
                            }

                        DeviceListView(appState: appState)
                            .tabItem {
                                Label("Macs", systemImage: "desktopcomputer")
                            }

                        SettingsView(appState: appState)
                            .tabItem {
                                Label("Settings", systemImage: "gearshape")
                            }
                    }
                }
            }
            .task {
                #if os(iOS)
                appDelegate.appState = appState
                #endif
                appState.loadAndConnect()
            }
        }
    }
}
