Create one horizontal animation strip for Codex pet `iroha-doctoral`, state `running-right`.

Use the attached canonical base for identity. Use the attached layout guide only for slot count, spacing, centering, and padding; do not draw the guide.

Output exactly 8 full-body frames in one left-to-right row on flat pure magenta #FF00FF. Treat the row as 8 invisible equal-width slots: one centered complete pose per slot, evenly spaced, with no overlap, clipping, empty slots, labels, or borders.

Identity: same pet in every frame: 酒寄彩叶博士研究生形象，严格依据用户参考设计：短圆润深紫色波波头，鲜明青绿色内层挑染，蓝灰色眼睛，温柔聪慧神情，纤细青年女性比例；白色科研大褂覆盖水手领浅色上衣与蓝绿色半裙，白袜黑色乐福鞋，米色斜挎包与粉色挂件。所有动画状态保持发型、配色、白大褂、包和挂件完全一致。. Preserve silhouette, face, proportions, markings, palette, material, style, and props.
Style: Pet-safe sprite: compact full-body mascot, readable in a 192x208 cell, clear silhouette, simple face, stable palette/materials, and crisp edges for chroma-key extraction. Style `sticker`: Polished sticker mascot with bold clean shapes, crisp outline, flat colors, and minimal highlight detail. User style notes: Polished Japanese anime chibi sticker sprite, clean dark outline, flat cel shading, expressive face, crisp opaque edges, no text, no shadows, no scenery..
Animation continuity: keep apparent pet scale and baseline stable within the row unless the state itself intentionally changes vertical position, such as `jumping`. Move the pose within the slot instead of redrawing the pet larger or smaller frame to frame.

State action: Dragging-right loop: show directional movement to the right through body and limb poses only.

State requirements:
- Show directional drag movement to the right through body, limb, and prop movement only.
- The row must unmistakably face and travel right.
- The movement cadence must alternate visibly across the 8 frames instead of repeating one nearly static stride.
- Do not draw speed lines, dust clouds, floor shadows, motion trails, or detached motion effects.

Clean extraction: crisp opaque edges, safe padding, no scenery, text, guide marks, checkerboard, shadows, glows, motion blur, speed lines, dust, detached effects, stray pixels, or chroma-key colors inside the pet.
