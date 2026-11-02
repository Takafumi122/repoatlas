"use client";

import React from "react";
import type { GuidedTourStep } from "@/lib/types";

interface GuidedTourBarProps {
  steps: GuidedTourStep[];
  selectedNodeId: string | null;
  onSelectStep: (nodeId: string) => void;
}

export function GuidedTourBar({
  steps,
  selectedNodeId,
  onSelectStep,
}: GuidedTourBarProps) {
  if (steps.length === 0) {
    return null;
  }

  const currentIdx = steps.findIndex((s) => s.node_id === selectedNodeId);
  const activeIndex = currentIdx >= 0 ? currentIdx : 0;
  const activeStep = steps[activeIndex];

  const handlePrev = () => {
    const prevIdx = activeIndex > 0 ? activeIndex - 1 : steps.length - 1;
    onSelectStep(steps[prevIdx].node_id);
  };

  const handleNext = () => {
    const nextIdx = activeIndex < steps.length - 1 ? activeIndex + 1 : 0;
    onSelectStep(steps[nextIdx].node_id);
  };

  return (
    <section
      className="guided-tour-bar"
      aria-label="Guided Code Tour"
      data-testid="guided-tour-bar"
    >
      <div className="tour-main-row">
        <div className="tour-title-group">
          <span className="badge badge-tour">
            Guided Code Tour ({activeIndex + 1} / {steps.length})
          </span>
          <span className="badge badge-role" data-testid="tour-role-label">
            {activeStep.role_label}
          </span>
          <button
            type="button"
            className="tour-current-file-btn"
            onClick={() => onSelectStep(activeStep.node_id)}
            data-testid="tour-current-path"
          >
            Step {activeStep.step}: <strong>{activeStep.path}</strong>
          </button>
          <span className="tour-reason-text" data-testid="tour-current-reason">
            推奨理由: {activeStep.reason}
          </span>
        </div>

        <div className="tour-nav-group">
          <button
            type="button"
            className="chip-btn tour-nav-btn"
            onClick={handlePrev}
            data-testid="tour-prev-btn"
            aria-label="前の推奨ファイル (Previous)"
          >
            ← Previous
          </button>
          <button
            type="button"
            className="chip-btn tour-nav-btn"
            onClick={handleNext}
            data-testid="tour-next-btn"
            aria-label="次の推奨ファイル (Next)"
          >
            Next →
          </button>
        </div>
      </div>

      <div className="tour-steps-strip">
        {steps.map((s, idx) => {
          const isCurrent = selectedNodeId === s.node_id;
          return (
            <button
              key={s.node_id}
              type="button"
              className={`tour-step-pill ${
                isCurrent ? "tour-step-pill-active" : ""
              }`}
              onClick={() => onSelectStep(s.node_id)}
              title={`Step ${s.step} [${s.role_label}] ${s.path} — ${s.reason}`}
              data-testid={`tour-step-${idx + 1}`}
            >
              <span className="tour-step-num">#{s.step}</span>
              <span className="tour-step-name">
                {s.path.split("/").pop() || s.path}
              </span>
            </button>
          );
        })}
      </div>
    </section>
  );
}
