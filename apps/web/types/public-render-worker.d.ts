declare module '#public-render-worker-url' {
  const workerUrl: string;
  export default workerUrl;
}

declare module '#public-render-validator' {
  const validate: (value: unknown) => boolean;
  export default validate;
}
