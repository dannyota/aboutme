export type FieldIntent<T>
  = | { readonly kind: 'set'; readonly value: T }
    | { readonly kind: 'unset' };
