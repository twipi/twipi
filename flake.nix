{
	inputs = {
		nixpkgs.url = "github:nixos/nixpkgs/nixos-unstable";
		flake-utils.url = "github:numtide/flake-utils";
	};

	outputs = { self, nixpkgs, flake-utils }:
		flake-utils.lib.eachDefaultSystem (system:
			with nixpkgs.legacyPackages.${system}.extend (self: super: {
				# Keep this in sync with go.mod.
				go = super.go_1_21;
			});
			{
				devShell = mkShell {
					buildInputs = [
						go
						gopls
						gotools
					];
				};
			}
		);
}
