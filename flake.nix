{
  description = "git-work: a project tracker stored in git, with Jira sync";

  inputs.nixpkgs.url = "github:NixOS/nixpkgs/nixos-unstable";

  outputs =
    { self, nixpkgs }:
    let
      systems = [
        "aarch64-darwin"
        "x86_64-darwin"
        "aarch64-linux"
        "x86_64-linux"
      ];
      forAllSystems = f: nixpkgs.lib.genAttrs systems (system: f nixpkgs.legacyPackages.${system});

      # A Nix build sees no .git, so it cannot `git describe`: the version is
      # VERSION, with the commit as build metadata. A tag v<VERSION> marks the
      # commit that sets it (CONTRIBUTING.md, "Versions").
      version = nixpkgs.lib.trim (builtins.readFile ./VERSION);
      rev = self.shortRev or self.dirtyShortRev or "unknown";
    in
    {
      packages = forAllSystems (pkgs: {
        default = pkgs.buildGoModule {
          pname = "git-work";
          inherit version;
          src = self;

          # Changes whenever go.sum does: set it to pkgs.lib.fakeHash, run
          # `nix build`, and copy the hash it reports.
          vendorHash = "sha256-aMX8IKZPE3bdhfYUeuLvlgye+nLWqC6Ywejn9WYKZXI=";

          subPackages = [ "." ];
          env.CGO_ENABLED = 0;
          ldflags = [
            "-s"
            "-w"
            "-X main.version=v${version}+${rev}"
          ];

          # CI runs the tests; they need git and take minutes.
          doCheck = false;

          nativeBuildInputs = [ pkgs.installShellFiles ];

          # The module path is github.com/git-bug/git-bug, so go names the
          # binary git-bug.
          postInstall = ''
            mv $out/bin/git-bug $out/bin/git-work
            installManPage doc/man/*.1
            installShellCompletion --cmd git-work \
              --bash misc/completion/bash/git-work \
              --zsh misc/completion/zsh/git-work \
              --fish misc/completion/fish/git-work
          '';

          meta = {
            description = "Project tracker stored in git, with Jira sync";
            homepage = "https://github.com/chef-robotics/git-work";
            license = pkgs.lib.licenses.gpl3Plus;
            mainProgram = "git-work";
          };
        };
      });

      devShells = forAllSystems (pkgs: {
        default = pkgs.mkShell {
          packages = [
            pkgs.go
            pkgs.git
          ];
        };
      });
    };
}
