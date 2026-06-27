import { forwardRef, type ButtonHTMLAttributes, type CSSProperties } from "react";
import { cva, type VariantProps } from "class-variance-authority";
import { cn } from "./lib/cn.js";

const buttonVariants = cva(
  "atomyx-btn inline-flex items-center justify-center whitespace-nowrap font-medium focus-visible:outline-none disabled:pointer-events-none disabled:opacity-50",
  {
    variants: {
      variant: {
        primary: "atomyx-btn-primary",
        secondary: "atomyx-btn-secondary",
        ghost: "atomyx-btn-ghost",
        destructive: "atomyx-btn-destructive",
      },
      size: {
        sm: "",
        md: "",
        lg: "",
      },
    },
    defaultVariants: {
      variant: "primary",
      size: "md",
    },
  },
);

const SIZE_STYLE: Record<
  NonNullable<VariantProps<typeof buttonVariants>["size"]>,
  CSSProperties
> = {
  sm: {
    height: "22px",
    padding: "0 var(--gap-3)",
    fontSize: "var(--fs-12)",
    borderRadius: "var(--r-2)",
    gap: "var(--gap-2)",
  },
  md: {
    height: "26px",
    padding: "0 var(--gap-4)",
    fontSize: "var(--fs-12)",
    borderRadius: "var(--r-2)",
    gap: "var(--gap-2)",
  },
  lg: {
    height: "32px",
    padding: "0 var(--gap-5)",
    fontSize: "var(--fs-13)",
    borderRadius: "var(--r-3)",
    gap: "var(--gap-3)",
  },
};

export type ButtonProps = ButtonHTMLAttributes<HTMLButtonElement> &
  VariantProps<typeof buttonVariants>;

export const Button = forwardRef<HTMLButtonElement, ButtonProps>(
  ({ className, variant, size, style, ...props }, ref) => {
    const s = size ?? "md";
    return (
      <button
        ref={ref}
        className={cn(buttonVariants({ variant, size: s }), className)}
        style={{ ...SIZE_STYLE[s], ...style }}
        {...props}
      />
    );
  },
);
Button.displayName = "Button";
