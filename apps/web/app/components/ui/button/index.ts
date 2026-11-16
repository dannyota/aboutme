import type { VariantProps } from 'class-variance-authority';
import { cva } from 'class-variance-authority';

export { default as Button } from './Button.vue';

export const buttonVariants = cva(
  'inline-flex items-center justify-center gap-2 whitespace-nowrap rounded-full text-sm transition-colors disabled:pointer-events-none disabled:opacity-50 [&_svg]:pointer-events-none [&_svg:not([class*=\'size-\'])]:size-4 shrink-0 [&_svg]:shrink-0 outline-none focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-2 focus-visible:ring-offset-background aria-invalid:border-destructive',
  {
    variants: {
      variant: {
        default:
          'bg-primary font-bold text-primary-foreground hover:bg-primary-hover',
        seal: 'bg-seal font-bold text-seal-foreground hover:bg-brand-hover',
        destructive:
          'border-[1.5px] border-destructive bg-transparent font-bold text-destructive hover:bg-destructive/10',
        outline:
          'border-[1.5px] border-foreground bg-transparent font-bold text-foreground hover:bg-accent hover:text-accent-foreground',
        secondary:
          'bg-secondary font-bold text-secondary-foreground hover:bg-secondary/80',
        ghost:
          'font-bold hover:bg-accent hover:text-accent-foreground',
        link: 'font-semibold text-link underline-offset-4 hover:underline focus-visible:underline',
      },
      size: {
        'default': 'h-10 px-[18px] py-2 has-[>svg]:px-4',
        'xs': 'h-6 gap-1 px-2 text-xs has-[>svg]:px-1.5 [&_svg:not([class*=\'size-\'])]:size-3',
        'sm': 'h-9 gap-1.5 px-3.5 has-[>svg]:px-3',
        'lg': 'h-12 px-6 has-[>svg]:px-5',
        'icon': 'size-9',
        'icon-xs': 'size-6 [&_svg:not([class*=\'size-\'])]:size-3',
        'icon-sm': 'size-8',
        'icon-lg': 'size-10',
      },
    },
    defaultVariants: {
      variant: 'default',
      size: 'default',
    },
  },
);
export type ButtonVariants = VariantProps<typeof buttonVariants>;
