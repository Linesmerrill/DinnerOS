import SwiftUI

/// "Sign in with email" under the Apple and Google buttons, for the App Review account only
/// (decision 752). A small text button opens the email and password fields; the screen shows
/// it only while the server has that account set up.
struct EmailSignInSection: View {
    @Bindable var model: SignInModel
    @FocusState private var focus: Field?

    private enum Field { case email, password }

    var body: some View {
        if model.isEmailFormOpen {
            form
        } else {
            Button("Sign in with email") {
                model.isEmailFormOpen = true
                focus = .email
            }
            .buttonStyle(.dsTextLink)
            .accessibilityIdentifier("signIn.email.open")
        }
    }

    private var form: some View {
        VStack(spacing: DS.Space.s) {
            TextField("Email", text: $model.email)
                .textContentType(.username)
                .keyboardType(.emailAddress)
                .textInputAutocapitalization(.never)
                .autocorrectionDisabled()
                .submitLabel(.next)
                .focused($focus, equals: .email)
                .onSubmit { focus = .password }
                .textFieldStyle(.roundedBorder)
                .accessibilityIdentifier("signIn.email.address")
            SecureField("Password", text: $model.password)
                .textContentType(.password)
                .submitLabel(.go)
                .focused($focus, equals: .password)
                .onSubmit { Task { await model.signInWithEmail() } }
                .textFieldStyle(.roundedBorder)
                .accessibilityIdentifier("signIn.email.password")
            Button {
                focus = nil
                Task { await model.signInWithEmail() }
            } label: {
                Text("Sign In")
            }
            .buttonStyle(.dsSecondary(fullWidth: true))
            .disabled(!model.canSubmitEmail)
            .accessibilityIdentifier("signIn.email.submit")
        }
        .padding(.top, DS.Space.xxs)
    }
}
