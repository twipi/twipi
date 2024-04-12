{
	inputs = {
		nixpkgs.url = "github:nixos/nixpkgs/nixos-unstable";
		flake-utils.url = "github:numtide/flake-utils";
	};

	outputs = { self, nixpkgs, flake-utils }:
		flake-utils.lib.eachDefaultSystem (system:
			with nixpkgs.legacyPackages.${system}.extend (self: super: {
				# Keep this in sync with go.mod.
				go = super.go_1_22;
			});
			{
				devShell = mkShell {
					packages = [
						go
						gopls
						gotools

						protolint
						protobuf
						protoc-gen-go
						protoc-gen-doc

						go-task
						process-compose
					];

					PC_PORT_NUM = "38475";
				};
			}
		);
}
