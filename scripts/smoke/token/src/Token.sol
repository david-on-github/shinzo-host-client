// SPDX-License-Identifier: MIT
pragma solidity ^0.8.20;

/// Minimal ERC-20 shaped contract for smoke tests: it only emits Transfer
/// events. anvil_setCode places its runtime bytecode at the mainnet USDC
/// address so the production USDC lens fixture sees matching logs.
contract Token {
    event Transfer(address indexed from, address indexed to, uint256 value);

    function transfer(address to, uint256 value) external returns (bool) {
        emit Transfer(msg.sender, to, value);
        return true;
    }
}
