declare module "vue" {
  export interface GlobalComponents {
    MButton: (typeof import("./components/ui"))["MButton"];
    MChip: (typeof import("./components/ui"))["MChip"];
    MEmptyState: (typeof import("./components/ui"))["MEmptyState"];
    MPageHeader: (typeof import("./components/ui"))["MPageHeader"];
    MSelect: (typeof import("./components/ui"))["MSelect"];
    MStatus: (typeof import("./components/ui"))["MStatus"];
    MSurface: (typeof import("./components/ui"))["MSurface"];
  }
}

export {};
