import React, { Component, ReactNode } from 'react';

interface Props {
  children: ReactNode;
}

interface State {
  hasError: boolean;
  error: Error | null;
}

export class ErrorBoundary extends Component<Props, State> {
  declare props: Props;
  declare state: State;

  constructor(props: Props) {
    super(props);
    this.state = {
      hasError: false,
      error: null,
    };
  }

  public static getDerivedStateFromError(error: Error): State {
    return { hasError: true, error };
  }

  public componentDidCatch(error: Error, errorInfo: React.ErrorInfo) {
    console.error('Uncaught error in React App:', error, errorInfo);
  }

  public render() {
    if (this.state.hasError) {
      return (
        <div className="min-h-screen bg-[#f5f5f0] text-[#1a1a15] p-8 flex flex-col items-center justify-center font-sans">
          <div className="bg-white p-8 rounded-3xl border border-[#e2e2d5] max-w-lg w-full text-center shadow-lg space-y-4">
            <div className="w-12 h-12 rounded-2xl bg-rose-100 text-rose-700 font-bold flex items-center justify-center mx-auto text-xl">
              !
            </div>
            <h1 className="font-serif text-2xl font-bold text-[#1a1a15]">Произошла ошибка при отображении</h1>
            <p className="text-xs text-[#8c8c7a] font-mono bg-[#f5f5f0] p-3 rounded-xl break-words text-left">
              {this.state.error?.message || 'Неизвестная ошибка интерфейса'}
            </p>
            <button
              onClick={() => {
                localStorage.clear();
                window.location.reload();
              }}
              className="px-6 py-2.5 rounded-full bg-[#5A5A40] text-white text-xs font-bold hover:bg-[#484833] transition-colors cursor-pointer shadow-xs"
            >
              Сбросить кэш и перезагрузить
            </button>
          </div>
        </div>
      );
    }

    return this.props.children;
  }
}
