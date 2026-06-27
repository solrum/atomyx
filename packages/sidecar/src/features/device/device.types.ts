export interface DeviceDescriptor {
  readonly id: string;
  readonly platform: "android" | "ios";
  readonly name: string;
  readonly kind: "simulator" | "emulator" | "device";
  readonly state: "online" | "offline" | "unauthorized";
}

export interface DeviceEvent {
  readonly kind: "connected" | "disconnected" | "state-changed";
  readonly device: DeviceDescriptor;
}
