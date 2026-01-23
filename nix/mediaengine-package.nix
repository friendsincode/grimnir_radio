{ lib
, buildGoModule
, protobuf
, protoc-gen-go
, protoc-gen-go-grpc
, pkg-config
, gst_all_1
, makeWrapper
}:

buildGoModule rec {
  pname = "grimnir-radio-mediaengine";
  version = "1.0.0";

  src = lib.cleanSource ./..;

  vendorHash = null; # Will be updated after first build

  subPackages = [ "cmd/mediaengine" ];

  nativeBuildInputs = [
    protobuf
    protoc-gen-go
    protoc-gen-go-grpc
    pkg-config
    makeWrapper
  ];

  buildInputs = with gst_all_1; [
    gstreamer
    gst-plugins-base
    gst-plugins-good
    gst-plugins-bad
    gst-plugins-ugly
    gst-libav
  ];

  preBuild = ''
    # Generate protobuf code if needed
    if [ -d proto ]; then
      export PATH=$PATH:${protoc-gen-go}/bin:${protoc-gen-go-grpc}/bin
      make proto || true
    fi
  '';

  postInstall = ''
    # Wrap binary to ensure GStreamer plugins are found
    wrapProgram $out/bin/mediaengine \
      --prefix GST_PLUGIN_SYSTEM_PATH_1_0 : "$GST_PLUGIN_SYSTEM_PATH_1_0" \
      --prefix GST_PLUGIN_PATH_1_0 : "$GST_PLUGIN_PATH_1_0"
  '';

  ldflags = [
    "-s"
    "-w"
    "-X main.version=${version}"
    "-X main.commit=${src.rev or "unknown"}"
    "-X main.date=1970-01-01T00:00:00Z"
  ];

  # Skip tests during build
  doCheck = false;

  meta = with lib; {
    description = "Modern broadcast automation system - Media Engine (GStreamer)";
    homepage = "https://github.com/friendsincode/grimnir_radio";
    license = licenses.agpl3Plus;
    maintainers = with maintainers; [ ];
    mainProgram = "mediaengine";
    platforms = platforms.linux; # GStreamer is Linux-focused
  };
}
