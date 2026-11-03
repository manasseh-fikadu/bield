// Button styles, shared by <Button> and by links and bits-ui triggers that
// should look like buttons.

export type ButtonVariant = 'primary' | 'secondary' | 'ghost' | 'danger';
export type ButtonSize = 'xs' | 'sm' | 'md';

// Buttons press down a pixel while held.
const base =
	'inline-flex shrink-0 items-center justify-center whitespace-nowrap transition-[color,background-color,border-color,translate] duration-[120ms] active:translate-y-px disabled:pointer-events-none disabled:opacity-50 aria-disabled:pointer-events-none aria-disabled:opacity-50';

const variants: Record<ButtonVariant, string> = {
	primary: 'border border-transparent bg-fg font-medium text-canvas hover:bg-fg/85',
	secondary:
		'border border-line-strong bg-transparent text-fg hover:border-line-heavy hover:bg-hover',
	ghost: 'border border-transparent bg-transparent text-fg-muted hover:bg-hover hover:text-fg',
	danger: 'border border-transparent bg-red font-medium text-canvas hover:bg-red/85'
};

const sizes: Record<ButtonSize, string> = {
	xs: 'h-6.5 gap-1 rounded-[5px] px-2.25 text-[12px] pointer-coarse:h-8',
	sm: 'h-7.5 gap-1.5 rounded-md px-2.75 text-[13px] pointer-coarse:h-10',
	md: 'h-9 gap-2 rounded-lg px-3.5 text-[14px] pointer-coarse:h-11'
};

/** The classes of a button. */
export function buttonClass(variant: ButtonVariant = 'secondary', size: ButtonSize = 'sm'): string {
	return `${base} ${sizes[size]} ${variants[variant]}`;
}

export type IconButtonSize = 'sm' | 'md';

// Fingers need more room than a mouse: on a touch screen a button grows.
const iconSizes: Record<IconButtonSize, string> = {
	sm: 'size-7 pointer-coarse:size-9',
	md: 'size-8 pointer-coarse:size-10'
};

/** The classes of a square button that holds only an icon. */
export function iconButtonClass(size: IconButtonSize = 'md'): string {
	return `inline-flex shrink-0 items-center justify-center rounded-md text-fg-subtle transition-colors hover:bg-hover hover:text-fg disabled:pointer-events-none disabled:opacity-50 ${iconSizes[size]}`;
}

/** The classes of a text field. */
export const fieldClass =
	'w-full rounded-md border border-line-strong bg-transparent px-3 text-[14px] text-fg transition-colors placeholder:text-fg-faint hover:border-line-heavy focus:border-fg-faint focus:outline-none disabled:opacity-50';
