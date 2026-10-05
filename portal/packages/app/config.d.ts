export interface Config {
  app: {
    /**
     * Sign-in providers on the sign-in page: 'microsoft' (hosted portal) or 'guest' (local).
     * @visibility frontend
     */
    signInProviders?: string[];
  };
}
