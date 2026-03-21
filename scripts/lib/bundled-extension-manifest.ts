export type ExtensionPackageJson = {
  name?: string;
  version?: string;
  dependencies?: Record<string, string>;
  optionalDependencies?: Record<string, string>;
  deneb?: {
    install?: {
      npmSpec?: string;
    };
  };
};

export type BundledExtension = { id: string; packageJson: ExtensionPackageJson };

export function collectBundledExtensionManifestErrors(extensions: BundledExtension[]): string[] {
  const errors: string[] = [];

  for (const extension of extensions) {
    const install = extension.packageJson.deneb?.install;
    if (
      install &&
      (!install.npmSpec || typeof install.npmSpec !== "string" || !install.npmSpec.trim())
    ) {
      errors.push(
        `bundled extension '${extension.id}' manifest invalid | deneb.install.npmSpec must be a non-empty string`,
      );
    }
  }

  return errors;
}
