{ lib
, buildGoModule
, fetchFromGitHub
, protobuf
, protoc-gen-go
, protoc-gen-go-grpc
}:

buildGoModule rec {
  pname = "grimnir-radio";
  version = "1.0.0";

  src = lib.cleanSource ./..;

  vendorHash = null; # Will be updated after first build

  subPackages = [ "cmd/grimnirradio" ];

  nativeBuildInputs = [
    protobuf
    protoc-gen-go
    protoc-gen-go-grpc
  ];

  preBuild = ''
    # Generate protobuf code if needed
    if [ -d proto ]; then
      export PATH=$PATH:${protoc-gen-go}/bin:${protoc-gen-go-grpc}/bin
      make proto || true
    fi
  '';

  ldflags = [
    "-s"
    "-w"
    "-X main.version=${version}"
    "-X main.commit=${src.rev or "unknown"}"
    "-X main.date=1970-01-01T00:00:00Z"
  ];

  # Skip tests during build (run them separately in CI)
  doCheck = false;

  meta = with lib; {
    description = "Modern broadcast automation system - Control Plane";
    homepage = "https://github.com/friendsincode/grimnir_radio";
    license = licenses.agpl3Plus;
    maintainers = with maintainers; [ ];
    mainProgram = "grimnirradio";
    platforms = platforms.linux ++ platforms.darwin;
  };
}
