import { createFrontendModule } from '@backstage/frontend-plugin-api';
import { SignInPageBlueprint } from '@backstage/plugin-app-react';
import { SignInPage } from '@backstage/core-components';
import {
  configApiRef,
  microsoftAuthApiRef,
  useApi,
} from '@backstage/core-plugin-api';
import type { SignInPageProps } from '@backstage/plugin-app-react';

const entra = {
  id: 'microsoft-auth-provider',
  title: 'Microsoft Entra ID',
  message: 'Sign in with your Microsoft Entra ID account',
  apiRef: microsoftAuthApiRef,
};

/**
 * The hosted portal signs in with Microsoft Entra ID; locally it's guest.
 * app.signInProviders (app-config) chooses: ['microsoft'] or ['guest'].
 */
function PlatformSignInPage(props: SignInPageProps) {
  const config = useApi(configApiRef);
  const providers =
    config.getOptionalStringArray('app.signInProviders') ?? ['guest'];
  return (
    <SignInPage
      {...props}
      providers={providers.map(p => (p === 'microsoft' ? entra : 'guest'))}
    />
  );
}

export const signInModule = createFrontendModule({
  pluginId: 'app',
  extensions: [
    SignInPageBlueprint.make({
      params: { loader: async () => PlatformSignInPage },
    }),
  ],
});
