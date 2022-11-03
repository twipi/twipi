let overlay = self: super: {
	go = super.go_1_19;
};

in { pkgs ? import <nixpkgs> { overlays = [ overlay ]; } }:

let lib = pkgs.lib;

in pkgs.mkShell {
	name = "twikit";

	buildInputs = with pkgs; [
		go
		gopls
		gotools
		sqlc
	];

	TWIDISCORD_DEBUG = "1";
}
